package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// okHandler always returns 200; the middleware wraps it.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// doN sends n requests with the given Authorization header and returns the
// status codes. A wide window keeps all requests inside one window.
func doN(h http.Handler, auth string, n int) []int {
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v0/pipes/x.json", nil)
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes[i] = rec.Code
	}
	return codes
}

func TestLimitTrips(t *testing.T) {
	h := PerToken(2, WithWindow(time.Minute))(okHandler)
	codes := doN(h, "tokenA", 3)

	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("first two should pass, got %v", codes)
	}
	if codes[2] != http.StatusTooManyRequests {
		t.Fatalf("third should be 429, got %d", codes[2])
	}
}

func TestKeysIndependent(t *testing.T) {
	h := PerToken(1, WithWindow(time.Minute))(okHandler)

	// tokenA exhausts its single allowance.
	if got := doN(h, "tokenA", 2); got[1] != http.StatusTooManyRequests {
		t.Fatalf("tokenA second should be 429, got %v", got)
	}
	// tokenB is unaffected.
	if got := doN(h, "tokenB", 1); got[0] != http.StatusOK {
		t.Fatalf("tokenB should pass, got %v", got)
	}
}

func TestDisabledWhenZero(t *testing.T) {
	h := PerToken(0)(okHandler)
	for _, c := range doN(h, "tokenA", 100) {
		if c != http.StatusOK {
			t.Fatalf("limiting disabled but got %d", c)
		}
	}
}

func TestCustomKeyFn(t *testing.T) {
	// Key everything to a constant -> all requests share one bucket.
	h := PerToken(1, WithWindow(time.Minute), WithKeyFn(func(_ *http.Request) (string, error) {
		return "shared", nil
	}))(okHandler)

	codes := doN(h, "", 2)
	if codes[0] != http.StatusOK || codes[1] != http.StatusTooManyRequests {
		t.Fatalf("shared key should trip on second request, got %v", codes)
	}
}
