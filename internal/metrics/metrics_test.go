package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// scrape renders the metrics exposition text from a Metrics instance.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v0/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestHandlerServes(t *testing.T) {
	out := scrape(t, New())
	// Label-less Counters emit at zero; *Vec collectors emit nothing until a
	// label set is observed (covered by the middleware test), so assert on the
	// always-present concrete counters here.
	for _, name := range []string{
		"tinyraven_events_ingested_total",
		"tinyraven_events_quarantined_total",
	} {
		if !strings.Contains(out, name) {
			t.Fatalf("exposition missing %q", name)
		}
	}
}

func TestMiddlewareRecordsRouteAndStatus(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/v0/pipes/{name}.json", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v0/pipes/foo.json")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	out := scrape(t, m)
	// Route label is the pattern, not the raw path (cardinality control).
	if !strings.Contains(out, `route="/v0/pipes/{name}.json"`) {
		t.Fatalf("request counter missing route pattern label:\n%s", out)
	}
	if !strings.Contains(out, `status="200"`) {
		t.Fatalf("status label not captured:\n%s", out)
	}
	// Pipe name recorded from URL param.
	if !strings.Contains(out, `tinyraven_pipe_requests_total{pipe="foo",status="200"}`) {
		t.Fatalf("pipe counter missing:\n%s", out)
	}
}

func TestMiddlewareCapturesNon200(t *testing.T) {
	m := New()
	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(r)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if out := scrape(t, m); !strings.Contains(out, `status="500"`) {
		t.Fatalf("non-200 status not captured:\n%s", out)
	}
}

func TestIngestObserved(t *testing.T) {
	m := New()
	m.IngestObserved(7, 3)

	out := scrape(t, m)
	if !strings.Contains(out, "tinyraven_events_ingested_total 7") {
		t.Fatalf("ingested counter not 7:\n%s", out)
	}
	if !strings.Contains(out, "tinyraven_events_quarantined_total 3") {
		t.Fatalf("quarantined counter not 3:\n%s", out)
	}
}
