package sqlproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/clickhouse"
)

type fakeQuerier struct {
	gotSQL      string
	gotSettings map[string]string
	body        []byte
	err         error
}

func (f *fakeQuerier) Query(_ context.Context, sql string, _, settings map[string]string) ([]byte, error) {
	f.gotSQL = sql
	f.gotSettings = settings
	return f.body, f.err
}

func TestSuccessPassthrough(t *testing.T) {
	q := &fakeQuerier{body: []byte(`{"data":[]}`)}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/sql?q=SELECT+1+FORMAT+JSON", nil)
	New(q).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"data":[]}` {
		t.Fatalf("body = %q, want passthrough", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	// FORMAT already present -> not double-appended.
	if strings.Count(strings.ToUpper(q.gotSQL), "FORMAT") != 1 {
		t.Fatalf("FORMAT appended twice: %q", q.gotSQL)
	}
	// readonly + caps applied.
	if q.gotSettings["readonly"] != "2" {
		t.Fatalf("readonly setting = %q, want 2", q.gotSettings["readonly"])
	}
}

func TestFormatJSONAppended(t *testing.T) {
	q := &fakeQuerier{body: []byte(`{}`)}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/sql?q=SELECT+1", nil)
	New(q).ServeHTTP(rec, req)

	if !strings.HasSuffix(q.gotSQL, "FORMAT JSON") {
		t.Fatalf("sql = %q, want trailing FORMAT JSON", q.gotSQL)
	}
}

func TestPostRawBody(t *testing.T) {
	q := &fakeQuerier{body: []byte(`{}`)}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v0/sql", strings.NewReader("SELECT 2"))
	New(q).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.HasPrefix(q.gotSQL, "SELECT 2") {
		t.Fatalf("sql = %q, want raw body", q.gotSQL)
	}
}

func TestPostFormBody(t *testing.T) {
	q := &fakeQuerier{body: []byte(`{}`)}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v0/sql", strings.NewReader("q=SELECT+3"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	New(q).ServeHTTP(rec, req)

	if !strings.HasPrefix(q.gotSQL, "SELECT 3") {
		t.Fatalf("sql = %q, want form-decoded q", q.gotSQL)
	}
}

func TestMissingQ(t *testing.T) {
	q := &fakeQuerier{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/sql", nil)
	New(q).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if q.gotSQL != "" {
		t.Fatalf("querier called with %q, want no call", q.gotSQL)
	}
}

func TestCHErrorMapping(t *testing.T) {
	cases := []struct {
		code       int
		wantStatus int
	}{
		{62, http.StatusBadRequest},         // SYNTAX_ERROR
		{60, http.StatusNotFound},           // UNKNOWN_TABLE
		{0, http.StatusInternalServerError}, // no code extracted
	}
	for _, tc := range cases {
		q := &fakeQuerier{err: &clickhouse.CHError{Code: tc.code, Msg: "boom"}}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/sql?q=SELECT+1", nil)
		New(q).ServeHTTP(rec, req)

		if rec.Code != tc.wantStatus {
			t.Fatalf("code %d -> status %d, want %d", tc.code, rec.Code, tc.wantStatus)
		}
		if tc.code != 0 {
			if h := rec.Header().Get("X-DB-Exception-Code"); h == "" {
				t.Fatalf("code %d: missing X-DB-Exception-Code header", tc.code)
			}
		}
		if !strings.Contains(rec.Body.String(), "boom") {
			t.Fatalf("code %d: body %q missing message", tc.code, rec.Body.String())
		}
	}
}

func TestGenericError(t *testing.T) {
	q := &fakeQuerier{err: io.ErrUnexpectedEOF}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/sql?q=SELECT+1", nil)
	New(q).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
