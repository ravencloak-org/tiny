// Command querybench is a stdlib-only query load generator for TinyRaven pipe
// endpoints. Where scripts/loadtest measures the ingestion path (events/s), this
// measures the read path: how fast published pipes answer under concurrency.
//
// It spins up N workers, each looping for a fixed duration issuing
// GET /v0/pipes/<pipe>.json?<param> with a bearer token, and reports query
// throughput plus latency percentiles. Two latencies are tracked:
//
//   - client wall latency (full HTTP round trip, p50/p95/p99)
//   - ClickHouse server-side time, parsed from the response body's
//     statistics.elapsed (FORMAT JSON), reported p50/p95 when present
//
// The -distinct flag controls the working set of param values, which is how we
// separate cached from uncached latency. Pipes opt into ClickHouse's query_cache
// via CACHE_TTL (ADR 0009); repeated identical-param queries then hit the cache:
//
//	-distinct 1      all requests share one param value -> max cache hits (cached)
//	-distinct 100000 values rarely repeat -> mostly cache misses (uncached)
//
// Usage:
//
//	go run ./scripts/querybench \
//	  -url http://localhost:8010 -token "$TR_READ_TOKEN" \
//	  -pipe user_metrics -params user_id=u -workers 50 -duration 15s -distinct 1
//
// See docs/benchmark.md for methodology and the reference figures.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var (
		baseURL  = flag.String("url", "http://localhost:8010", "TinyRaven base URL")
		token    = flag.String("token", "", "bearer token (a read-scoped token or TR_ADMIN_TOKEN)")
		pipe     = flag.String("pipe", "user_metrics", "pipe endpoint name (served at /v0/pipes/<pipe>.json)")
		params   = flag.String("params", "user_id=u", "rotating query param as key=value; value gets a 0..distinct-1 suffix")
		workers  = flag.Int("workers", 50, "number of concurrent workers")
		duration = flag.Duration("duration", 15*time.Second, "how long to run, e.g. 15s, 1m")
		distinct = flag.Int("distinct", 1, "distinct param-value combos to rotate through (1 = all-same -> max cache hits)")
		timeout  = flag.Duration("timeout", 30*time.Second, "per-request HTTP timeout")
	)
	flag.Parse()

	if *workers < 1 || *duration <= 0 || *distinct < 1 {
		fmt.Fprintln(os.Stderr, "workers must be >= 1, distinct >= 1, and duration > 0")
		os.Exit(2)
	}

	// ponytail: -params is a single key=value pair (the example pipe takes one
	// {{String(user_id)}}). The value is the rotation base; we never split on '&'.
	key, baseVal, ok := strings.Cut(*params, "=")
	if !ok || key == "" {
		fmt.Fprintln(os.Stderr, "-params must be key=value, e.g. user_id=u")
		os.Exit(2)
	}

	// Precompute the rotation set once so the hot loop just indexes a slice.
	urls := make([]string, *distinct)
	prefix := fmt.Sprintf("%s/v0/pipes/%s.json?%s=", *baseURL, *pipe, url.QueryEscape(key))
	for i := range urls {
		val := baseVal
		if *distinct > 1 {
			val = baseVal + strconv.Itoa(i) // distinct values -> cache misses
		}
		urls[i] = prefix + url.QueryEscape(val)
	}

	// One client with a keep-alive pool sized to the worker count, so the
	// benchmark measures query serving rather than TCP/TLS handshakes.
	tr := &http.Transport{
		MaxIdleConns:        *workers * 2,
		MaxIdleConnsPerHost: *workers * 2,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{Transport: tr, Timeout: *timeout}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	mode := "uncached (cache misses)"
	if *distinct == 1 {
		mode = "cached (max cache hits)"
	}
	fmt.Printf("querybench: %d workers -> %s/v0/pipes/%s.json [%s, distinct=%d] for %s\n",
		*workers, *baseURL, *pipe, mode, *distinct, *duration)

	var (
		reqOK  atomic.Int64
		reqErr atomic.Int64
	)

	clientLats := make([][]time.Duration, *workers) // full round-trip per request
	serverLats := make([][]time.Duration, *workers) // statistics.elapsed, when present
	var wg sync.WaitGroup
	start := time.Now()

	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cl := make([]time.Duration, 0, 4096)
			sl := make([]time.Duration, 0, 4096)
			i := id % *distinct // stagger workers across the rotation set
			for {
				if ctx.Err() != nil {
					break
				}
				t0 := time.Now()
				ok, elapsed := doQuery(ctx, client, urls[i], *token)
				cl = append(cl, time.Since(t0))

				if ok {
					reqOK.Add(1)
					if elapsed >= 0 {
						sl = append(sl, elapsed)
					}
				} else {
					reqErr.Add(1)
				}

				i++
				if i >= *distinct {
					i = 0
				}
			}
			clientLats[id] = cl
			serverLats[id] = sl
		}(w)
	}

	wg.Wait()
	elapsed := time.Since(start)

	client50, client95, client99 := merge(clientLats)
	server50, server95, _ := merge(serverLats)

	total := reqOK.Load() + reqErr.Load()
	qps := float64(total) / elapsed.Seconds()

	fmt.Printf("\n--- results ---\n")
	fmt.Printf("%-22s %s\n", "elapsed:", elapsed.Round(time.Millisecond))
	fmt.Printf("%-22s ok=%d err=%d\n", "requests:", reqOK.Load(), reqErr.Load())
	fmt.Printf("%-22s %.0f queries/s\n", "throughput:", qps)
	fmt.Printf("\n%-22s %-12s %-12s %-12s\n", "latency", "p50", "p95", "p99")
	fmt.Printf("%-22s %-12s %-12s %-12s\n", "  client (wall):", ms(client50), ms(client95), ms(client99))
	if hasAny(serverLats) {
		fmt.Printf("%-22s %-12s %-12s %-12s\n", "  CH elapsed:", ms(server50), ms(server95), "-")
	} else {
		fmt.Printf("%-22s (no statistics.elapsed in responses)\n", "  CH elapsed:")
	}

	if reqErr.Load() > 0 {
		os.Exit(1) // surface query failures to CI / the shell
	}
}

// doQuery issues one GET and returns whether it was a 2xx plus the ClickHouse
// server-side time parsed from the response body's statistics.elapsed. A
// negative elapsed means the field was absent (e.g. an error body).
func doQuery(ctx context.Context, c *http.Client, url, token string) (ok bool, elapsed time.Duration) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, -1
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return false, -1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, -1
	}
	return true, jsonElapsed(body)
}

// jsonElapsed pulls statistics.elapsed (a float, in seconds) out of a ClickHouse
// FORMAT JSON body without decoding the whole {"meta":..,"data":..} payload.
// Hand-rolled like loadtest's jsonInt to keep the parser off the alloc hot path;
// "elapsed" only appears inside "statistics" in CH's JSON envelope. Returns a
// negative duration if absent.
func jsonElapsed(body []byte) time.Duration {
	needle := `"elapsed"`
	i := strings.Index(string(body), needle)
	if i < 0 {
		return -1
	}
	j := i + len(needle)
	for j < len(body) && (body[j] == ':' || body[j] == ' ') {
		j++
	}
	k := j
	for k < len(body) && (body[k] == '-' || body[k] == '.' || body[k] == 'e' || body[k] == 'E' ||
		body[k] == '+' || (body[k] >= '0' && body[k] <= '9')) {
		k++
	}
	secs, err := strconv.ParseFloat(string(body[j:k]), 64)
	if err != nil || secs < 0 {
		return -1
	}
	return time.Duration(secs * float64(time.Second))
}

// merge flattens per-worker latency slices, sorts once, and returns p50/p95/p99.
func merge(per [][]time.Duration) (p50, p95, p99 time.Duration) {
	var all []time.Duration
	for _, r := range per {
		all = append(all, r...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	return pct(all, 50), pct(all, 95), pct(all, 99)
}

// hasAny reports whether any worker recorded a server-side elapsed sample.
func hasAny(per [][]time.Duration) bool {
	for _, r := range per {
		if len(r) > 0 {
			return true
		}
	}
	return false
}

// pct returns the nearest-rank pth percentile of a sorted slice (same definition
// as scripts/loadtest, so the two benchmarks report comparable numbers).
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

// ms renders a duration as milliseconds with microsecond resolution.
func ms(d time.Duration) string {
	return fmt.Sprintf("%.3fms", float64(d.Microseconds())/1000.0)
}
