package api

import (
	_ "embed"
	"net/http"
)

// docsHTML is the human-browsable API docs page, embedded into the binary so
// self-hosted deployments work air-gapped with no CDN or external fetch (ADR
// 0017 amendment). The page is a single self-contained file: on load it
// fetches /v0/openapi.json and renders the deployment's live pipe endpoints.
//
// ponytail: this is a deliberately small vanilla-JS renderer, not a vendored
// Redoc/Scalar bundle (~1 MB) — sufficient for endpoint discovery while keeping
// binary growth tiny. Swapping in Redoc or Scalar standalone later is trivial:
// drop their single-file bundle next to this one and change the //go:embed
// target below; DocsHandler keeps the same signature so the mount point and the
// off-by-default gate in server.go are untouched.
//
//go:embed docs_ui.html
var docsHTML []byte

// DocsHandler serves the embedded API docs page as text/html. It is a plain
// http.Handler so the orchestrator can mount it at /tr/v1/docs and gate it
// behind a config flag (off by default per ADR 0017); this handler reads no
// config of its own. The page renders the runtime spec at /v0/openapi.json
// entirely client-side, so nothing here needs the pipe registry.
func DocsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(docsHTML)
	})
}
