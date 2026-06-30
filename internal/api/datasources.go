package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/tinyraven/tinyraven/internal/model"
)

// handleListDatasources serves GET /v0/datasources — the Tinybird datasource
// listing/introspection endpoint. It returns the registered datasources the
// caller's token can READ (ADMIN sees all), wrapped in Tinybird's
// {"datasources":[...]} envelope so existing clients (tb CLI / SDK introspection)
// work unchanged. The per-token subset matches Tinybird, which narrows the list
// rather than 403-ing it. READ:<ds> is the scope primitive (docs/parity-gaps.md).
func (s *server) handleListDatasources(w http.ResponseWriter, r *http.Request) {
	dss, err := s.deps.Datasources.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list datasources: "+err.Error())
		return
	}

	tok, _ := tokenFrom(r.Context())
	// Non-nil empty slice so the JSON is {"datasources":[]} (not null) when empty.
	items := make([]dsItem, 0, len(dss))
	for _, ds := range dss {
		if allow(tok, "READ", ds.Name) {
			items = append(items, toDSItem(ds))
		}
	}
	encodeJSON(w, http.StatusOK, dsListResp{Datasources: items})
}

// handleGetDatasource serves GET /v0/datasources/{name} — single datasource
// detail (schema + engine). READ:<ds> scoped (ADMIN sees all); returns the
// datasource object directly (unwrapped), mirroring the list DTO and Tinybird.
// 403 when the token lacks READ for the datasource (checked first), 404 when the
// name is unknown.
func (s *server) handleGetDatasource(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if tok, _ := tokenFrom(r.Context()); !allow(tok, "READ", name) {
		writeError(w, http.StatusForbidden, "token lacks READ scope for datasource: "+name)
		return
	}
	ds, ok, err := s.deps.Datasources.Get(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not get datasource: "+err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "datasource not found: "+name)
		return
	}
	encodeJSON(w, http.StatusOK, toDSItem(ds))
}

// dsListResp is the Tinybird-shaped envelope for GET /v0/datasources.
type dsListResp struct {
	Datasources []dsItem `json:"datasources"`
}

// dsItem is one datasource as Tinybird reports it. We expose the fields a client
// needs to discover a schema (name, columns, engine); ids/timestamps/statistics
// are intentionally omitted rather than fabricated (docs/parity-gaps.md).
type dsItem struct {
	Name    string     `json:"name"`
	Engine  dsEngine   `json:"engine"`
	Columns []dsColumn `json:"columns"`
}

type dsEngine struct {
	Engine string `json:"engine"`
}

type dsColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// toDSItem maps a parsed datasource to its API shape. nullable is derived from
// the ClickHouse type (Nullable(...)), matching how Tinybird flags it.
func toDSItem(ds *model.Datasource) dsItem {
	cols := make([]dsColumn, len(ds.Schema))
	for i, c := range ds.Schema {
		cols[i] = dsColumn{
			Name:     c.Name,
			Type:     c.Type,
			Nullable: strings.HasPrefix(c.Type, "Nullable("),
		}
	}
	return dsItem{
		Name:    ds.Name,
		Engine:  dsEngine{Engine: ds.Engine},
		Columns: cols,
	}
}
