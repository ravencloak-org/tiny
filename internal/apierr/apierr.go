// Package apierr is the shared HTTP response envelope: the Tinybird-compatible
// error shape (ADR 0012) and JSON write helpers. Centralized so every handler —
// core routes and the /v0/sql, /v0/metrics, etc. add-ons — emits an identical
// structure and status mapping.
package apierr

import (
	"encoding/json"
	"net/http"
)

// DBExceptionHeader passes the ClickHouse exception code through to the client
// (ADR 0012). Set it from a clickhouse.CHError when one is available.
const DBExceptionHeader = "X-DB-Exception-Code"

type body struct {
	Error string `json:"error"`
}

// WriteError sends {"error": msg} with the given status (ADR 0012).
func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body{Error: msg})
}

// WriteErrorWithCode is WriteError plus the X-DB-Exception-Code header.
func WriteErrorWithCode(w http.ResponseWriter, status, dbCode int, msg string) {
	if dbCode != 0 {
		w.Header().Set(DBExceptionHeader, itoa(dbCode))
	}
	WriteError(w, status, msg)
}

// WriteJSON sends a pre-encoded JSON body verbatim (e.g. a ClickHouse response).
func WriteJSON(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// EncodeJSON marshals v and sends it with the given status.
func EncodeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
