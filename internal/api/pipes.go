package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handlePipe executes a published pipe endpoint and returns its JSON result
// (ADR 0003). Query params become validated {{Type(name)}} values; the pipe SQL
// is authoritative for LIMIT/format (no framework-injected LIMIT, ADR 0025).
func (s *server) handlePipe(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusNotFound, "pipe not found")
		return
	}
	body, status, err := s.deps.Pipes.Run(r.Context(), name, r.URL.Query())
	if err != nil {
		// Runner sets status for client-mappable failures; default to 500.
		if status == 0 {
			status = http.StatusInternalServerError
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, status, body)
}
