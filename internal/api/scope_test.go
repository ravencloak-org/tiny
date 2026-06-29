package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

type fakeIngest struct{}

func (fakeIngest) Ingest(_ context.Context, _ string, rows []json.RawMessage) (int, int, error) {
	return len(rows), 0, nil
}

type fakePipes struct{}

func (fakePipes) Run(context.Context, string, url.Values) ([]byte, int, error) {
	return []byte(`{"data":[]}`), http.StatusOK, nil
}

type fakeTokens struct{ m map[string]*model.Token }

func (f fakeTokens) Validate(_ context.Context, v string) (*model.Token, bool, error) {
	t, ok := f.m[v]
	return t, ok, nil
}
func (fakeTokens) Put(context.Context, *model.Token) error { return nil }

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

// TestScopeEnforcement verifies that token scopes gate /v0/events, /v0/pipes,
// and /v0/sql (ADR 0005): ADMIN is a superuser; READ:<pipe>/APPEND:<ds> are
// resource-scoped; an unrelated scope is rejected with 403.
func TestScopeEnforcement(t *testing.T) {
	tokens := fakeTokens{m: map[string]*model.Token{
		"adm": {Name: "adm", Value: "adm", Scopes: []string{"ADMIN"}},
		"rd":  {Name: "rd", Value: "rd", Scopes: []string{"READ:user_metrics"}},
		"ap":  {Name: "ap", Value: "ap", Scopes: []string{"APPEND:events"}},
	}}
	h := New(Deps{
		Ingester:  fakeIngest{},
		Pipes:     fakePipes{},
		Tokens:    tokens,
		RedisPing: okPinger{},
		CHPing:    okPinger{},
		SQLProxy:  http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	})

	cases := []struct {
		name, method, path, token string
		body                      string
		want                      int
	}{
		{"events admin", "POST", "/v0/events?name=events", "adm", `{"a":1}`, 202},
		{"events append-match", "POST", "/v0/events?name=events", "ap", `{"a":1}`, 202},
		{"events read-token denied", "POST", "/v0/events?name=events", "rd", `{"a":1}`, 403},
		{"events append-wrong-ds", "POST", "/v0/events?name=other", "ap", `{"a":1}`, 403},
		{"events no token", "POST", "/v0/events?name=events", "", `{"a":1}`, 401},
		{"events bad token", "POST", "/v0/events?name=events", "nope", `{"a":1}`, 403},
		{"pipe admin", "GET", "/v0/pipes/user_metrics.json", "adm", "", 200},
		{"pipe read-match", "GET", "/v0/pipes/user_metrics.json", "rd", "", 200},
		{"pipe read-wrong", "GET", "/v0/pipes/other.json", "rd", "", 403},
		{"pipe append-token denied", "GET", "/v0/pipes/user_metrics.json", "ap", "", 403},
		{"sql admin", "GET", "/v0/sql?q=SELECT+1", "adm", "", 200},
		{"sql non-admin denied", "GET", "/v0/sql?q=SELECT+1", "rd", "", 403},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body *strings.Reader = strings.NewReader(c.body)
			req := httptest.NewRequest(c.method, c.path, body)
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("%s %s [%s] = %d, want %d (body %s)", c.method, c.path, c.token, rec.Code, c.want, rec.Body.String())
			}
		})
	}
}
