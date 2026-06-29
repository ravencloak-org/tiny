package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDocsHandler verifies the embedded docs page serves as text/html, looks
// like the rendered HTML page, and points the client at the runtime spec
// endpoint /tr/v1/openapi.json (ADR 0017 amendment). No network is touched: the
// page fetches the spec client-side, so the handler just returns the asset.
func TestDocsHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tr/v1/docs", nil)

	DocsHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{"<html", "TinyRaven API Docs", "/tr/v1/openapi.json"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}
