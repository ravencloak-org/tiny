package api

import (
	"net/http"

	"github.com/tinyraven/tinyraven/internal/apierr"
)

// Thin package-local aliases over the shared apierr helpers (ADR 0012), so
// handler call sites stay terse.

func writeError(w http.ResponseWriter, status int, msg string) {
	apierr.WriteError(w, status, msg)
}

func writeJSON(w http.ResponseWriter, status int, raw []byte) {
	apierr.WriteJSON(w, status, raw)
}

func encodeJSON(w http.ResponseWriter, status int, v any) {
	apierr.EncodeJSON(w, status, v)
}
