package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// pipeRouter mounts PerPipe on a chi route that captures {name}, so
// chi.URLParam(r, "name") resolves exactly as it does in the real server.
func pipeRouter(mw func(http.Handler) http.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.With(mw).Get("/v0/pipes/{name}.json", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return r
}

// hit sends one request for the given pipe + token and returns the status code.
func hit(h http.Handler, pipe, auth string) int {
	req := httptest.NewRequest(http.MethodGet, "/v0/pipes/"+pipe+".json", nil)
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func hitN(h http.Handler, pipe, auth string, n int) []int {
	codes := make([]int, n)
	for i := range codes {
		codes[i] = hit(h, pipe, auth)
	}
	return codes
}

// A pipe with RATE_LIMIT 2 lets 2 through, then 429s; other pipes/tokens
// have their own independent windows.
func TestPerPipe_PerPipeLimitTrips(t *testing.T) {
	limits := map[string]int{"slow": 2} // "fast" has no limit -> use default
	limitFor := func(p string) int { return limits[p] }

	// defaultRPS 0 so only "slow" is limited; window wide to stay in one window.
	h := pipeRouter(PerPipe(0, limitFor, WithWindow(time.Minute)))

	codes := hitN(h, "slow", "tokenA", 3)
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("first two on slow should pass, got %v", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("third on slow should be 429, got %d", codes[2])
	}

	// A different pipe (no limit, default 0 => unlimited) is unaffected.
	for i, c := range hitN(h, "fast", "tokenA", 5) {
		if c != http.StatusOK {
			t.Fatalf("fast request %d should pass (unlimited), got %d", i, c)
		}
	}

	// A different token on the SAME limited pipe has its own window.
	if got := hitN(h, "slow", "tokenB", 2); got[0] != 200 || got[1] != 200 {
		t.Fatalf("tokenB on slow should pass independently, got %v", got)
	}
}

// Two pipes that share the same effective rps (same underlying limiter) still
// get independent windows because the composite key includes the pipe name.
func TestPerPipe_SameRPSIndependentWindows(t *testing.T) {
	limitFor := func(string) int { return 2 } // every pipe limited to 2/window
	h := pipeRouter(PerPipe(0, limitFor, WithWindow(time.Minute)))

	if got := hitN(h, "alpha", "tok", 3); got[2] != http.StatusTooManyRequests {
		t.Fatalf("alpha third should 429, got %v", got)
	}
	// beta shares the rps=2 limiter but is a distinct key -> unaffected.
	if got := hitN(h, "beta", "tok", 2); got[0] != 200 || got[1] != 200 {
		t.Fatalf("beta should pass independently, got %v", got)
	}
}

// limitFor returning 0 falls back to defaultRPS.
func TestPerPipe_FallsBackToDefault(t *testing.T) {
	limitFor := func(string) int { return 0 } // no per-pipe limit anywhere
	h := pipeRouter(PerPipe(1, limitFor, WithWindow(time.Minute)))

	codes := hitN(h, "x", "tokenA", 2)
	if codes[0] != 200 || codes[1] != http.StatusTooManyRequests {
		t.Fatalf("default rps=1 should trip on second, got %v", codes)
	}
}

// Both default and per-pipe limits 0 => unlimited pass-through.
func TestPerPipe_BothZeroUnlimited(t *testing.T) {
	h := pipeRouter(PerPipe(0, func(string) int { return 0 }, WithWindow(time.Minute)))
	for i, c := range hitN(h, "x", "tokenA", 100) {
		if c != http.StatusOK {
			t.Fatalf("request %d should pass (unlimited), got %d", i, c)
		}
	}
}

// Per-pipe limit overrides a non-zero default for that pipe; unlisted pipes
// keep the default.
func TestPerPipe_OverridesDefault(t *testing.T) {
	limits := map[string]int{"big": 5}
	h := pipeRouter(PerPipe(1, func(p string) int { return limits[p] }, WithWindow(time.Minute)))

	// "big" overrides default(1) with 5.
	for i, c := range hitN(h, "big", "tok", 5) {
		if c != http.StatusOK {
			t.Fatalf("big request %d within limit 5 should pass, got %d", i, c)
		}
	}
	if c := hit(h, "big", "tok"); c != http.StatusTooManyRequests {
		t.Fatalf("big 6th should 429, got %d", c)
	}
	// "small" uses default(1).
	if got := hitN(h, "small", "tok", 2); got[1] != http.StatusTooManyRequests {
		t.Fatalf("small should use default rps=1, got %v", got)
	}
}

// Concurrent traffic across pipes/tokens must be race-clean and still enforce
// the per-pipe ceiling.
func TestPerPipe_ConcurrentRaceClean(t *testing.T) {
	h := pipeRouter(PerPipe(0, func(string) int { return 10 }, WithWindow(time.Minute)))

	var wg sync.WaitGroup
	var passed int64
	const workers, perWorker = 8, 50
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			pipe := "p" + string(rune('A'+id)) // each worker -> distinct pipe
			for i := 0; i < perWorker; i++ {
				if hit(h, pipe, "tok") == http.StatusOK {
					atomic.AddInt64(&passed, 1)
				}
			}
		}(w)
	}
	wg.Wait()

	// Each of the 8 distinct pipes has its own limit of 10 -> at most 80 pass.
	if passed > int64(workers*10) {
		t.Fatalf("passed %d exceeds aggregate ceiling %d", passed, workers*10)
	}
	if passed == 0 {
		t.Fatal("expected some requests to pass")
	}
}
