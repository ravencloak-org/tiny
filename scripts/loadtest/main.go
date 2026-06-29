// Command loadtest is a stdlib-only load generator for the TinyRaven events API.
//
// It spins up N concurrent workers, each POSTing batched NDJSON to
// /v0/events?name=<datasource> for a fixed duration, and reports throughput
// (events/s) and request-latency percentiles (p50/p95/p99). It measures the
// ack-on-buffer path (ADR 0004): a 202 means the rows were validated and
// buffered for the ClickHouse flush.
//
// Usage:
//
//	go run ./scripts/loadtest \
//	  -url http://localhost:8000 -token "$TR_ADMIN_TOKEN" \
//	  -datasource events -workers 50 -duration 30s -batch 1000
//
// See docs/benchmark.md for methodology and the reference figures.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var (
		baseURL    = flag.String("url", "http://localhost:8000", "TinyRaven base URL")
		token      = flag.String("token", "", "bearer token (TR_ADMIN_TOKEN or a write-scoped token)")
		datasource = flag.String("datasource", "events", "target datasource name (?name=)")
		workers    = flag.Int("workers", 50, "number of concurrent workers")
		duration   = flag.Duration("duration", 30*time.Second, "how long to run, e.g. 30s, 1m")
		batch      = flag.Int("batch", 1000, "events per request (NDJSON lines)")
		timeout    = flag.Duration("timeout", 30*time.Second, "per-request HTTP timeout")
	)
	flag.Parse()

	if *workers < 1 || *batch < 1 || *duration <= 0 {
		fmt.Fprintln(os.Stderr, "workers, batch must be >= 1 and duration > 0")
		os.Exit(2)
	}

	endpoint := fmt.Sprintf("%s/v0/events?name=%s", *baseURL, *datasource)

	// One client, generous keep-alive pool so workers reuse connections rather
	// than thrashing TCP/TLS handshakes — otherwise the benchmark measures dialing.
	tr := &http.Transport{
		MaxIdleConns:        *workers * 2,
		MaxIdleConnsPerHost: *workers * 2,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{Transport: tr, Timeout: *timeout}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	fmt.Printf("loadtest: %d workers x batch %d -> %s for %s\n", *workers, *batch, endpoint, *duration)

	var (
		events      atomic.Int64 // events submitted (sent in 202'd requests)
		successful  atomic.Int64 // successful_rows reported by the server
		quarantined atomic.Int64 // quarantined_rows reported by the server
		reqOK       atomic.Int64
		reqErr      atomic.Int64
	)

	results := make([][]time.Duration, *workers) // per-worker latencies, merged at end
	var wg sync.WaitGroup
	start := time.Now()

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Estimate capacity so the hot loop never reallocates the slice.
			lats := make([]time.Duration, 0, 4096)
			buf := &bytes.Buffer{}
			seq := 0
			for {
				if ctx.Err() != nil {
					break
				}
				buf.Reset()
				writeBatch(buf, id, &seq, *batch)

				t0 := time.Now()
				ok, succ, quar := postBatch(ctx, client, endpoint, *token, buf.Bytes())
				lats = append(lats, time.Since(t0))

				if ok {
					reqOK.Add(1)
					events.Add(int64(*batch))
					successful.Add(int64(succ))
					quarantined.Add(int64(quar))
				} else {
					reqErr.Add(1)
				}
			}
			results[id] = lats
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Merge and sort latencies for percentile math.
	var all []time.Duration
	for _, r := range results {
		all = append(all, r...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	evs := events.Load()
	eps := float64(evs) / elapsed.Seconds()

	fmt.Printf("\n--- results ---\n")
	fmt.Printf("elapsed:        %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("requests:       ok=%d err=%d\n", reqOK.Load(), reqErr.Load())
	fmt.Printf("events sent:    %d\n", evs)
	fmt.Printf("server rows:    successful=%d quarantined=%d\n", successful.Load(), quarantined.Load())
	fmt.Printf("throughput:     %.0f events/s\n", eps)
	if len(all) > 0 {
		fmt.Printf("latency p50:    %s\n", pct(all, 50).Round(time.Microsecond))
		fmt.Printf("latency p95:    %s\n", pct(all, 95).Round(time.Microsecond))
		fmt.Printf("latency p99:    %s\n", pct(all, 99).Round(time.Microsecond))
		fmt.Printf("latency max:    %s\n", all[len(all)-1].Round(time.Microsecond))
	}
	if reqErr.Load() > 0 {
		os.Exit(1) // surface request failures to CI / the shell
	}
}

// postBatch sends one NDJSON batch and returns whether it was accepted (202)
// plus the server's reported successful/quarantined row counts.
func postBatch(ctx context.Context, c *http.Client, url, token string, body []byte) (ok bool, successful, quarantined int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, 0, 0
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return false, 0, 0
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		return false, 0, 0
	}
	return true, jsonInt(b, "successful_rows"), jsonInt(b, "quarantined_rows")
}

// writeBatch appends n NDJSON event rows to buf. seq is advanced so rows are
// distinct across the run. Hand-rolled (no encoding/json) to keep the generator
// off the allocation hot path — the row shape matches examples/quickstart.
func writeBatch(buf *bytes.Buffer, worker int, seq *int, n int) {
	ts := time.Now().UTC().Format("2006-01-02 15:04:05") // ClickHouse DateTime
	for i := 0; i < n; i++ {
		*seq++
		fmt.Fprintf(buf,
			`{"event_id":"w%d-%d","user_id":"u%d","event":"pageview","timestamp":"%s"}`+"\n",
			worker, *seq, *seq%10000, ts)
	}
}

// pct returns the nearest-rank pth percentile of a sorted, non-empty slice.
func pct(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := (p*len(sorted) + 99) / 100 // ceil(p/100 * n)
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// jsonInt pulls an integer value for key out of a small flat JSON object body
// without pulling in encoding/json reflection — the body is always
// {"successful_rows":N,"quarantined_rows":M}.
func jsonInt(body []byte, key string) int {
	needle := []byte(`"` + key + `"`)
	i := bytes.Index(body, needle)
	if i < 0 {
		return 0
	}
	j := i + len(needle)
	for j < len(body) && (body[j] == ':' || body[j] == ' ') {
		j++
	}
	n, sign := 0, 1
	if j < len(body) && body[j] == '-' {
		sign, j = -1, j+1
	}
	for j < len(body) && body[j] >= '0' && body[j] <= '9' {
		n = n*10 + int(body[j]-'0')
		j++
	}
	return n * sign
}
