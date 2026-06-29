package pipe

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Executor runs published pipe endpoints against ClickHouse (model.PipeRunner).
// Query parameters are bound via ClickHouse's native {name:Type} placeholders,
// never string-interpolated, so endpoints are injection-proof (ADR 0003).
type Executor struct {
	ch    model.CHQuerier
	pipes model.PipeRegistry
	ds    model.DatasourceRegistry // reserved for future referential checks; unused in MVP Run
}

// NewExecutor wires the runner to its ClickHouse querier and registries.
func NewExecutor(ch model.CHQuerier, pipes model.PipeRegistry, ds model.DatasourceRegistry) *Executor {
	return &Executor{ch: ch, pipes: pipes, ds: ds}
}

var _ model.PipeRunner = (*Executor)(nil)

// Run executes the endpoint of the named pipe and returns the ClickHouse JSON
// body. status is the HTTP status the API should send; err carries the failure
// reason for client-mappable cases (ADR 0012).
func (e *Executor) Run(ctx context.Context, name string, params url.Values) (body []byte, status int, err error) {
	p, ok := e.pipes.Get(name)
	if !ok {
		return nil, http.StatusNotFound, fmt.Errorf("pipe not found: %s", name)
	}
	if p.Endpoint == nil {
		return nil, http.StatusNotFound, fmt.Errorf("pipe %q has no published endpoint", name)
	}

	sql := composeSQL(p)

	// Rewrite {{Type(name, default)}} -> {name:Type}. Defaults are handled when
	// building the bound-parameter map below, not in the SQL text.
	sql = placeholderRe.ReplaceAllString(sql, "{${2}:${1}}")

	chParams := make(map[string]string, len(p.Endpoint.Params))
	for _, param := range p.Endpoint.Params {
		switch {
		case params.Has(param.Name):
			chParams["param_"+param.Name] = params.Get(param.Name)
		case param.HasDefault:
			chParams["param_"+param.Name] = param.Default
		default:
			return nil, http.StatusBadRequest, fmt.Errorf("missing required parameter: %s", param.Name)
		}
	}

	settings := map[string]string{}
	if p.Endpoint.CacheTTL > 0 { // ADR 0009: opt-in query result cache
		settings["use_query_cache"] = "1"
		settings["query_cache_ttl"] = strconv.Itoa(p.Endpoint.CacheTTL)
	}

	// FORMAT JSON yields ClickHouse's {"meta":[...],"data":[...],...} shape,
	// compatible with Tinybird's /v0/pipes/{name}.json response for MVP.
	sql = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(sql), ";")) + "\nFORMAT JSON"

	body, err = e.ch.Query(ctx, sql, chParams, settings)
	if err != nil {
		// ADR 0012: surface CH errors as 400 with the body so the API maps them.
		return body, http.StatusBadRequest, err
	}
	return body, http.StatusOK, nil
}

// composeSQL builds the final query. Upstream nodes become CTEs in file order;
// the endpoint SQL is the final SELECT and references them by name.
// ponytail: naive composition — assumes node SQLs are plain SELECTs, that the
// endpoint already FROMs the node names, and that file order is a valid
// dependency order. Sufficient for MVP single/linear pipes.
func composeSQL(p *model.Pipe) string {
	endpointSQL := strings.TrimSpace(p.Endpoint.SQL)
	if len(p.Nodes) == 0 {
		return endpointSQL
	}
	ctes := make([]string, len(p.Nodes))
	for i, n := range p.Nodes {
		ctes[i] = n.Name + " AS (" + strings.TrimSpace(n.SQL) + ")"
	}
	return "WITH " + strings.Join(ctes, ", ") + " " + endpointSQL
}
