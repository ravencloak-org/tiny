package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStatusReachable points `tr status` at a fake server via TINYBIRD_HOST and
// checks it reports the host reachable.
func TestStatusReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("HOME", t.TempDir()) // avoid reading a real ~/.tinyraven/config.yml
	t.Setenv("TINYBIRD_HOST", srv.URL)

	cmd := newStatusCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, srv.URL) {
		t.Fatalf("output missing host:\n%s", out)
	}
	if !strings.Contains(out, "reachable") {
		t.Fatalf("expected reachable:\n%s", out)
	}
}
