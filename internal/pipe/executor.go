package pipe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Executor runs published pipe endpoints against ClickHouse (model.PipeRunner).
// Query parameters are bound via ClickHouse's native {name:Type} placeholders,
// never string-interpolated, so endpoints are injection-proof (ADR 0003). Each
// run emits a best-effort observability stat to a StatsRecorder (ADR 0014).
type Executor struct {
	ch    model.CHQuerier
	pipes model.PipeRegistry
	ds    model.DatasourceRegistry // reserved for future referential checks; unused in MVP Run
	rec   model.StatsRecorder      // may be nil — observability is optional and never blocks
}

// NewExecutor wires the runner to its ClickHouse querier, registries, and the
// stats recorder (rec may be nil; ADR 0014 — observability is best-effort).
func NewExecutor(ch model.CHQuerier, pipes model.PipeRegistry, ds model.DatasourceRegistry, rec model.StatsRecorder) *Executor {
	return &Executor{ch: ch, pipes: pipes, ds: ds, rec: rec}
}

var _ model.PipeRunner = (*Executor)(nil)

// uuidRe validates the canonical 8-4-4-4-12 hex UUID form for UUID params.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

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

	// From here a query against a known endpoint is attempted; record a stat on
	// every exit path (ADR 0014). The recorder is contractually non-blocking, so
	// this never slows or alters the response.
	var durMS float64
	var readRows, readBytes int64
	defer func() {
		if e.rec == nil {
			return
		}
		e.rec.Record(model.QueryStat{
			Pipe:       name,
			DurationMS: durMS,
			ReadRows:   readRows,
			ReadBytes:  readBytes,
			StatusCode: status,
			Error:      errString(err),
		})
	}()

	sql := composeSQL(p)

	// Rewrite {{Type(name, default)}} -> {name:Type}, mapping the template type to
	// its ClickHouse parameter type. Defaults bind in the param map below, not in
	// the SQL text — values are never interpolated (ADR 0003).
	sql = placeholderRe.ReplaceAllStringFunc(sql, func(match string) string {
		m := placeholderRe.FindStringSubmatch(match)
		return "{" + m[2] + ":" + chParamType(model.ParamType(m[1])) + "}"
	})

	chParams := make(map[string]string, len(p.Endpoint.Params))
	for _, param := range p.Endpoint.Params {
		var raw string
		switch {
		case params.Has(param.Name):
			raw = params.Get(param.Name)
		case param.HasDefault:
			raw = param.Default
		default:
			return nil, http.StatusBadRequest, fmt.Errorf("missing required parameter: %s", param.Name)
		}
		norm, verr := normalizeParam(param.Type, param.Name, raw)
		if verr != nil {
			return nil, http.StatusBadRequest, verr
		}
		chParams["param_"+param.Name] = norm
	}

	settings := map[string]string{}
	if p.Endpoint.CacheTTL > 0 { // ADR 0009: opt-in query result cache
		settings["use_query_cache"] = "1"
		settings["query_cache_ttl"] = strconv.Itoa(p.Endpoint.CacheTTL)
	}

	// FORMAT JSON yields ClickHouse's {"meta":[...],"data":[...],...} shape,
	// compatible with Tinybird's /v0/pipes/{name}.json response for MVP.
	sql = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(sql), ";")) + "\nFORMAT JSON"

	start := time.Now()
	body, err = e.ch.Query(ctx, sql, chParams, settings)
	durMS = float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		// ADR 0012: surface CH errors as 400 with the body so the API maps them.
		return body, http.StatusBadRequest, err
	}
	readRows, readBytes = parseStats(body)
	return body, http.StatusOK, nil
}

// chParamType maps a template parameter type to the ClickHouse type used in the
// {name:Type} placeholder (ADR 0003). Unlisted types pass through verbatim so
// any valid ClickHouse type (e.g. UInt64) still works without enumeration.
func chParamType(pt model.ParamType) string {
	switch pt {
	case "Boolean":
		// Values are normalized to 1/0 (see normalizeParam); UInt8 binds them
		// unambiguously and compares against Bool/UInt8 columns.
		return "UInt8"
	default:
		return string(pt)
	}
}

// normalizeParam validates and normalizes a raw parameter value (provided or
// default) for its template type, returning a 400-worthy error on bad input.
// Validation is type-shaped only; ClickHouse remains the authority on exact
// DateTime/Date formats. Output is bound as a CH parameter, never interpolated.
func normalizeParam(pt model.ParamType, name, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	switch pt {
	case "Int64":
		if _, err := strconv.ParseInt(v, 10, 64); err != nil {
			return "", fmt.Errorf("parameter %s must be an integer", name)
		}
		return v, nil
	case "Int32":
		if _, err := strconv.ParseInt(v, 10, 32); err != nil {
			return "", fmt.Errorf("parameter %s must be an integer", name)
		}
		return v, nil
	case "Float64":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return "", fmt.Errorf("parameter %s must be a number", name)
		}
		return v, nil
	case "Boolean":
		switch strings.ToLower(v) {
		case "true", "1":
			return "1", nil
		case "false", "0":
			return "0", nil
		default:
			return "", fmt.Errorf("parameter %s must be a boolean", name)
		}
	case "UUID":
		if !uuidRe.MatchString(v) {
			return "", fmt.Errorf("parameter %s must be a UUID", name)
		}
		return v, nil
	case "DateTime", "DateTime64", "Date":
		if v == "" {
			return "", fmt.Errorf("parameter %s must not be empty", name)
		}
		return raw, nil // pass through; ClickHouse validates the exact format
	default: // String and unlisted types
		return raw, nil
	}
}

// errString returns err's message, or "" when err is nil (QueryStat.Error).
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// parseStats best-effort-extracts read counters from a ClickHouse FORMAT JSON
// body's "statistics" object (ADR 0014). Any parse failure yields zeros — stats
// must never affect the response.
func parseStats(body []byte) (readRows, readBytes int64) {
	var r struct {
		Statistics struct {
			RowsRead  int64 `json:"rows_read"`
			BytesRead int64 `json:"bytes_read"`
		} `json:"statistics"`
	}
	_ = json.Unmarshal(body, &r)
	return r.Statistics.RowsRead, r.Statistics.BytesRead
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
