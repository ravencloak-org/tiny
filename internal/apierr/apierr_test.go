package apierr

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// WriteError must emit the Tinybird-compatible {"error": msg} envelope with the
// given status and a JSON content type (ADR 0012). json.Encoder appends a newline.
func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusNotFound, "no such pipe")

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got, want := rec.Body.String(), "{\"error\":\"no such pipe\"}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if h := rec.Header().Get(DBExceptionHeader); h != "" {
		t.Errorf("WriteError must not set %s, got %q", DBExceptionHeader, h)
	}
}

// WriteErrorWithCode sets X-DB-Exception-Code only when the CH code is non-zero,
// so a "0" header never reaches the client (ADR 0012).
func TestWriteErrorWithCode(t *testing.T) {
	t.Run("non-zero code sets header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteErrorWithCode(rec, http.StatusBadRequest, 241, "memory limit exceeded")

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if h := rec.Header().Get(DBExceptionHeader); h != "241" {
			t.Errorf("%s = %q, want 241", DBExceptionHeader, h)
		}
		if got, want := rec.Body.String(), "{\"error\":\"memory limit exceeded\"}\n"; got != want {
			t.Errorf("body = %q, want %q", got, want)
		}
	})

	t.Run("zero code omits header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		WriteErrorWithCode(rec, http.StatusBadRequest, 0, "bad request")
		if h := rec.Header().Get(DBExceptionHeader); h != "" {
			t.Errorf("zero dbCode must not set %s, got %q", DBExceptionHeader, h)
		}
	})
}

// WriteJSON passes a pre-encoded body through verbatim (the ClickHouse response
// path) — no re-marshaling, no added bytes.
func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	raw := []byte(`{"data":[{"x":1}],"rows":1}`)
	WriteJSON(rec, http.StatusOK, raw)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if rec.Body.String() != string(raw) {
		t.Errorf("body = %q, want verbatim %q", rec.Body.String(), raw)
	}
}

// EncodeJSON marshals an arbitrary value and writes it with the given status.
func TestEncodeJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	EncodeJSON(rec, http.StatusCreated, map[string]any{"token": "tr_abc", "scopes": []string{"ADMIN"}})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got, want := rec.Body.String(), "{\"scopes\":[\"ADMIN\"],\"token\":\"tr_abc\"}\n"; got != want {
		t.Errorf("body = %q, want %q (encoding/json sorts map keys)", got, want)
	}
}
