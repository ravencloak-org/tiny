package api

import (
	"net/http"
	"strings"

	"github.com/tinyraven/tinyraven/internal/model"
)

// handleListDatasources serves GET /v0/datasources — the Tinybird datasource
// listing/introspection endpoint. It returns every registered datasource with
// its columns and engine, wrapped in Tinybird's {"datasources":[...]} envelope
// so existing clients (tb CLI / SDK introspection) work unchanged.
//
// ADMIN-gated by the route (see server.go): enumerating every datasource schema
// is a privileged operation, mirroring /v0/sql. Tinybird instead returns a
// token-scope-filtered subset; that's a deferred follow-up (no READ-datasource
// scope primitive exists yet — see docs/parity-gaps.md).
func (s *server) handleListDatasources(w http.ResponseWriter, r *http.Request) {
	dss, err := s.deps.Datasources.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list datasources: "+err.Error())
		return
	}

	// Non-nil empty slice so the JSON is {"datasources":[]} (not null) when empty.
	items := make([]dsItem, 0, len(dss))
	for _, ds := range dss {
		items = append(items, toDSItem(ds))
	}
	encodeJSON(w, http.StatusOK, dsListResp{Datasources: items})
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
