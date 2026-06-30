package pipe

import (
	"context"
	"crypto/rand"
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
	ch     model.CHQuerier
	pipes  model.PipeRegistry
	ds     model.DatasourceRegistry // reserved for future referential checks; unused in MVP Run
	rec    model.StatsRecorder      // may be nil — observability is optional and never blocks
	writer model.CHWriter           // write path for copy pipes; nil -> RunCopy is unavailable
}

// NewExecutor wires the runner to its ClickHouse querier, registries, and the
// stats recorder (rec may be nil; ADR 0014 — observability is best-effort). The
// copy write path is off by default; call EnableCopy to wire it.
func NewExecutor(ch model.CHQuerier, pipes model.PipeRegistry, ds model.DatasourceRegistry, rec model.StatsRecorder) *Executor {
	return &Executor{ch: ch, pipes: pipes, ds: ds, rec: rec}
}

// EnableCopy wires the read-write path RunCopy needs (INSERT INTO ... SELECT for
// copy pipes). Kept off the constructor so existing query-only callers are
// unaffected. Returns e for chaining.
func (e *Executor) EnableCopy(w model.CHWriter) *Executor {
	e.writer = w
	return e
}

var (
	_ model.PipeRunner = (*Executor)(nil)
	_ model.CopyRunner = (*Executor)(nil)
)

// uuidRe validates the canonical 8-4-4-4-12 hex UUID form for UUID params.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Run executes the endpoint of the named pipe and returns the ClickHouse JSON
// body. It is the JSON shorthand for RunFormat (ADR 0003).
func (e *Executor) Run(ctx context.Context, name string, params url.Values) (body []byte, status int, err error) {
	return e.RunFormat(ctx, name, params, model.FormatJSON)
}

// RunFormat executes the endpoint of the named pipe and returns the body in the
// requested output format. status is the HTTP status the API should send; err
// carries the failure reason for client-mappable cases (ADR 0012). Only the
// trailing ClickHouse FORMAT varies by format — param binding, control flow and
// validation are identical across formats.
func (e *Executor) RunFormat(ctx context.Context, name string, params url.Values, format model.OutputFormat) (body []byte, status int, err error) {
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

	// Compose the published SQL (upstream nodes as CTEs) and bind the request
	// params: control flow resolves first, then {{Type(name)}} -> {name:Type} with
	// values bound as CH params, never interpolated (ADR 0003). Shared with RunCopy.
	sql, chParams, status, err := bindQuery(composeSQL(p), p.Endpoint.Params, params)
	if err != nil {
		return nil, status, err
	}

	settings := map[string]string{}
	if p.Endpoint.CacheTTL > 0 { // ADR 0009: opt-in query result cache
		settings["use_query_cache"] = "1"
		settings["query_cache_ttl"] = strconv.Itoa(p.Endpoint.CacheTTL)
	}

	// Append the ClickHouse FORMAT for the requested output (ADR 0003). FORMAT
	// JSON yields the {"meta":[...],"data":[...],...} envelope (Tinybird .json);
	// CSVWithNames/JSONEachRow produce the raw .csv/.ndjson bodies.
	sql = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(sql), ";")) + "\nFORMAT " + chFormat(format)

	start := time.Now()
	body, err = e.ch.Query(ctx, sql, chParams, settings)
	durMS = float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		// ADR 0012: surface CH errors as 400 with the body so the API maps them.
		return body, http.StatusBadRequest, err
	}
	// Read counters live in the FORMAT JSON "statistics" object; for csv/ndjson
	// they aren't present, so the stat records zero rows/bytes (best-effort, the
	// status/error/duration are still recorded). ponytail: acceptable delta.
	if format == model.FormatJSON {
		readRows, readBytes = parseStats(body)
	}
	return body, http.StatusOK, nil
}

// RunCopy triggers a TYPE copy pipe on demand: it composes the copy SQL, binds
// request params (identical pipeline to a query), and runs
// INSERT INTO <target> SELECT ... over the read-write path. Tinybird models this
// as an async job; TinyRaven runs it synchronously and returns a job-shaped body
// with a terminal status so existing copy clients parse the response unchanged.
//
// ponytail: synchronous execution + no /v0/jobs surface (gap #8 deferred). The
// returned job is already "done" (or surfaced as a 400 CH error), so there is
// nothing to poll; job_url is emitted for shape parity but points at the
// unimplemented jobs route.
func (e *Executor) RunCopy(ctx context.Context, name string, params url.Values) (body []byte, status int, err error) {
	p, ok := e.pipes.Get(name)
	if !ok {
		return nil, http.StatusNotFound, fmt.Errorf("pipe not found: %s", name)
	}
	if p.Copy == nil {
		return nil, http.StatusBadRequest, fmt.Errorf("pipe %q is not a copy pipe", name)
	}
	if e.writer == nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("copy not enabled: no write path configured")
	}

	sql, chParams, status, err := bindQuery(composeWith(p.Nodes, p.Copy.SQL), p.Copy.Params, params)
	if err != nil {
		return nil, status, err
	}
	// Strip a trailing ; so the SELECT slots cleanly after INSERT INTO <target>.
	sql = strings.TrimRight(strings.TrimSpace(sql), ";")
	insert := "INSERT INTO " + chIdent(p.Copy.TargetDatasource) + " " + sql

	start := time.Now()
	cerr := e.writer.InsertSelect(ctx, insert, chParams)
	durMS := float64(time.Since(start).Microseconds()) / 1000.0
	if e.rec != nil { // best-effort observability (ADR 0014), non-blocking
		st := http.StatusOK
		if cerr != nil {
			st = http.StatusBadRequest
		}
		e.rec.Record(model.QueryStat{Pipe: name, DurationMS: durMS, StatusCode: st, Error: errString(cerr)})
	}
	if cerr != nil {
		// ADR 0012: surface the CH error so the API maps it (matches the query path).
		return nil, http.StatusBadRequest, cerr
	}
	return copyJobBody(name, p.Copy.TargetDatasource), http.StatusOK, nil
}

// bindQuery composes the per-request CH query from a terminal SQL string and the
// pipe's declared params: resolve control flow, rewrite {{Type(name)}} ->
// {name:Type}, then bind/validate each surviving param as a CH parameter (never
// interpolated; ADR 0003). Shared by the query (RunFormat) and copy (RunCopy)
// paths. On a client error it returns status 400 and the reason; otherwise 200.
func bindQuery(sql string, declared []model.Param, params url.Values) (string, map[string]string, int, error) {
	// Control flow first (ADR 0003): non-taken branches and the placeholders inside
	// them are dropped here, so a param used only in a false branch is not required.
	resolved, err := resolveControlFlow(sql, params, declared)
	if err != nil {
		return "", nil, http.StatusBadRequest, err
	}
	required := paramNamesInSQL(resolved)

	resolved = placeholderRe.ReplaceAllStringFunc(resolved, func(match string) string {
		m := placeholderRe.FindStringSubmatch(match)
		return "{" + m[2] + ":" + chParamType(model.ParamType(m[1])) + "}"
	})

	chParams := make(map[string]string, len(declared))
	for _, param := range declared {
		if !required[param.Name] {
			continue // referenced only inside a non-taken branch (or not at all): skip
		}
		var raw string
		switch {
		case params.Has(param.Name):
			raw = params.Get(param.Name)
		case param.HasDefault:
			raw = param.Default
		default:
			return "", nil, http.StatusBadRequest, fmt.Errorf("missing required parameter: %s", param.Name)
		}
		norm, verr := normalizeParam(param.Type, param.Name, raw)
		if verr != nil {
			return "", nil, http.StatusBadRequest, verr
		}
		chParams["param_"+param.Name] = norm
	}
	return resolved, chParams, http.StatusOK, nil
}

// chIdent backtick-quotes a ClickHouse identifier, doubling embedded backticks.
// The copy target comes from a git-tracked .pipe file (TARGET_DATASOURCE), not
// request input, but quoting keeps the INSERT well-formed for names with '-' etc.
func chIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// copyJob / copyJobResp are the Tinybird-shaped copy-trigger response. The job
// has already run synchronously, so Status is terminal ("done").
type copyJobResp struct {
	ID       string  `json:"id"`
	JobID    string  `json:"job_id"`
	JobURL   string  `json:"job_url"`
	Status   string  `json:"status"`
	PipeName string  `json:"pipe_name"`
	Job      copyJob `json:"job"`
}

type copyJob struct {
	ID         string       `json:"id"`
	Kind       string       `json:"kind"`
	Status     string       `json:"status"`
	PipeName   string       `json:"pipe_name"`
	Datasource copyJobDSRef `json:"datasource"`
}

type copyJobDSRef struct {
	Name string `json:"name"`
}

// copyJobBody builds the JSON body for a completed on-demand copy.
func copyJobBody(pipe, target string) []byte {
	id := newJobID()
	resp := copyJobResp{
		ID:       id,
		JobID:    id,
		JobURL:   "/v0/jobs/" + id, // ponytail: jobs surface is deferred (gap #8)
		Status:   "done",
		PipeName: pipe,
		Job: copyJob{
			ID:         id,
			Kind:       "copy",
			Status:     "done",
			PipeName:   pipe,
			Datasource: copyJobDSRef{Name: target},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// newJobID returns a random UUID v4 string for a synthesized copy job.
func newJobID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Vanishingly unlikely; fall back to a time-based id rather than failing the
		// copy over an rng hiccup.
		return fmt.Sprintf("job_%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// chFormat maps an output format to the ClickHouse FORMAT clause. Unknown
// formats fall back to JSON (the handler only routes the known suffixes).
func chFormat(f model.OutputFormat) string {
	switch f {
	case model.FormatCSV:
		return "CSVWithNames" // header row, matching Tinybird's .csv
	case model.FormatNDJSON:
		return "JSONEachRow" // newline-delimited JSON objects
	case model.FormatParquet:
		return "Parquet" // binary columnar, matching Tinybird's .parquet
	default:
		return "JSON"
	}
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
	return composeWith(p.Nodes, p.Endpoint.SQL)
}

// composeWith builds a query from upstream nodes (as CTEs, in file order) and a
// terminal SELECT. Shared by the endpoint (RunFormat) and copy (RunCopy) paths.
func composeWith(nodes []model.Node, terminalSQL string) string {
	terminalSQL = strings.TrimSpace(terminalSQL)
	if len(nodes) == 0 {
		return terminalSQL
	}
	ctes := make([]string, len(nodes))
	for i, n := range nodes {
		ctes[i] = n.Name + " AS (" + strings.TrimSpace(n.SQL) + ")"
	}
	return "WITH " + strings.Join(ctes, ", ") + " " + terminalSQL
}
