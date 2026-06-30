package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tinyraven/tinyraven/internal/model"
)

// handlePipe executes a published pipe endpoint and returns its result in the
// given output format (ADR 0003). Query params become validated {{Type(name)}}
// values; the pipe SQL is authoritative for LIMIT/format (no framework-injected
// LIMIT, ADR 0025). The format is fixed per route (.json/.csv/.ndjson); only the
// ClickHouse FORMAT and content type differ — auth (READ scope), param parsing
// and validation are shared with the JSON path.
func (s *server) handlePipe(format model.OutputFormat) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if name == "" {
			writeError(w, http.StatusNotFound, "pipe not found")
			return
		}
		if tok, _ := tokenFrom(r.Context()); !allow(tok, "READ", name) {
			writeError(w, http.StatusForbidden, "token lacks READ scope for pipe: "+name)
			return
		}
		body, status, err := s.deps.Pipes.RunFormat(r.Context(), name, r.URL.Query(), format)
		if err != nil {
			// Runner sets status for client-mappable failures; default to 500.
			if status == 0 {
				status = http.StatusInternalServerError
			}
			writeError(w, status, err.Error())
			return
		}
		writeBody(w, status, contentTypeFor(format), body)
	}
}

// contentTypeFor maps an output format to its HTTP content type, matching
// Tinybird's responses for the .json/.csv/.ndjson pipe endpoints.
func contentTypeFor(f model.OutputFormat) string {
	switch f {
	case model.FormatCSV:
		// ponytail: Tinybird also sends a charset; we keep the bare type. The CSV
		// itself carries a header row (FORMAT CSVWithNames).
		return "text/csv"
	case model.FormatNDJSON:
		return "application/x-ndjson"
	default:
		return "application/json"
	}
}

// handleListPipes serves GET /v0/pipes — the Tinybird pipe listing/introspection
// endpoint. It returns every registered pipe (nodes, endpoint params) wrapped in
// Tinybird's {"pipes":[...]} envelope so clients (tb pipe ls / SDK discovery)
// work unchanged. ADMIN-gated by the route, mirroring /v0/datasources: a
// token-scope-filtered subset is the deferred follow-up (docs/parity-gaps.md).
func (s *server) handleListPipes(w http.ResponseWriter, _ *http.Request) {
	pipes := s.deps.PipeReg.List()
	// Non-nil empty slice so the JSON is {"pipes":[]} (not null) when empty.
	items := make([]pipeItem, 0, len(pipes))
	for _, p := range pipes {
		items = append(items, toPipeItem(p))
	}
	encodeJSON(w, http.StatusOK, pipeListResp{Pipes: items})
}

// handleGetPipe serves GET /v0/pipes/{name} (no extension) — single pipe
// definition (nodes, endpoint node, params). ADMIN-gated; returns the pipe
// object directly (unwrapped), matching Tinybird. 404 when the name is unknown.
func (s *server) handleGetPipe(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	p, ok := s.deps.PipeReg.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "pipe not found: "+name)
		return
	}
	encodeJSON(w, http.StatusOK, toPipeItem(p))
}

// pipeListResp is the Tinybird-shaped envelope for GET /v0/pipes.
type pipeListResp struct {
	Pipes []pipeItem `json:"pipes"`
}

// pipeItem is one pipe as Tinybird reports it. We expose the fields a client
// needs to discover an endpoint (name, type, nodes, the published endpoint
// node + its params); ids/timestamps/urls are intentionally omitted rather than
// fabricated (mirrors the datasource DTO, docs/parity-gaps.md).
type pipeItem struct {
	Name     string     `json:"name"`
	Type     string     `json:"type"`     // "endpoint" | "materialized" | "default"
	Endpoint *string    `json:"endpoint"` // published node name, null when none
	Nodes    []pipeNode `json:"nodes"`
}

type pipeNode struct {
	Name   string      `json:"name"`
	SQL    string      `json:"sql"`
	Params []pipeParam `json:"params"` // always non-nil ([] for non-endpoint nodes)
}

type pipeParam struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Default *string `json:"default"` // null when the param has no default
}

// toPipeItem maps a parsed pipe to its API shape. Upstream NODE blocks become
// nodes with empty params; the ENDPOINT becomes a final node carrying its
// extracted params, and its name is surfaced as the top-level "endpoint".
// ponytail: our parser/model only tracks params on the endpoint node, so
// upstream nodes always report params:[] even if their SQL has placeholders.
func toPipeItem(p *model.Pipe) pipeItem {
	nodes := make([]pipeNode, 0, len(p.Nodes)+1)
	for _, n := range p.Nodes {
		nodes = append(nodes, pipeNode{Name: n.Name, SQL: n.SQL, Params: []pipeParam{}})
	}

	var endpoint *string
	typ := "default"
	switch {
	case p.Endpoint != nil:
		typ = "endpoint"
		name := p.Endpoint.Name
		endpoint = &name
		params := make([]pipeParam, len(p.Endpoint.Params))
		for i, pm := range p.Endpoint.Params {
			var def *string
			if pm.HasDefault {
				d := pm.Default
				def = &d
			}
			params[i] = pipeParam{Name: pm.Name, Type: string(pm.Type), Default: def}
		}
		nodes = append(nodes, pipeNode{Name: p.Endpoint.Name, SQL: p.Endpoint.SQL, Params: params})
	case p.Material != nil:
		typ = "materialized"
		nodes = append(nodes, pipeNode{Name: p.Material.Name, SQL: p.Material.SQL, Params: []pipeParam{}})
	}

	return pipeItem{Name: p.Name, Type: typ, Endpoint: endpoint, Nodes: nodes}
}
