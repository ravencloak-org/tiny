package api

import (
	"encoding/json"
	"net/http"
)

// errorBody is the Tinybird-compatible error envelope (ADR 0012): a flat JSON
// object with a single "error" string. We match structure + status codes, not
// message text.
type errorBody struct {
	Error string `json:"error"`
}

// writeError sends {"error": msg} with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: msg})
}

// writeJSON sends a pre-encoded JSON body verbatim with the given status. Used
// for ClickHouse responses we pass through untouched.
func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// encodeJSON marshals v and sends it with the given status.
func encodeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
