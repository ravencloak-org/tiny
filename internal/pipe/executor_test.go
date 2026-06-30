package pipe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// fakeWriter records the INSERT it was asked to run (model.CHWriter) for the
// copy-pipe path; no real ClickHouse.
type fakeWriter struct {
	calls     int
	gotSQL    string
	gotParams map[string]string
	err       error
}

func (w *fakeWriter) InsertSelect(_ context.Context, sql string, params map[string]string) error {
	w.calls++
	w.gotSQL, w.gotParams = sql, params
	return w.err
}

// fakeCH records the SQL/params/settings it was asked to run and returns canned
// results — no real ClickHouse (model.CHQuerier).
type fakeCH struct {
	calls       int
	gotSQL      string
	gotParams   map[string]string
	gotSettings map[string]string
	body        []byte
	err         error
}

func (f *fakeCH) Query(_ context.Context, sql string, params, settings map[string]string) ([]byte, error) {
	f.calls++
	f.gotSQL, f.gotParams, f.gotSettings = sql, params, settings
	return f.body, f.err
}

// fakeRegistry is an in-memory model.PipeRegistry for isolated executor tests.
type fakeRegistry struct{ m map[string]*model.Pipe }

func (r fakeRegistry) Get(name string) (*model.Pipe, bool) { p, ok := r.m[name]; return p, ok }
func (r fakeRegistry) Put(p *model.Pipe)                   { r.m[p.Name] = p }
func (r fakeRegistry) List() []*model.Pipe {
	out := make([]*model.Pipe, 0, len(r.m))
	for _, p := range r.m {
		out = append(out, p)
	}
	return out
}

// fakeRecorder captures the stats the executor emits (model.StatsRecorder).
type fakeRecorder struct{ stats []model.QueryStat }

func (r *fakeRecorder) Record(s model.QueryStat) { r.stats = append(r.stats, s) }

func mustParse(t *testing.T, name, raw string) *model.Pipe {
	t.Helper()
	p, err := Parse(name, raw)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return p
}

func newExec(ch model.CHQuerier, pipes ...*model.Pipe) *Executor {
	reg := fakeRegistry{m: map[string]*model.Pipe{}}
	for _, p := range pipes {
		reg.m[p.Name] = p
	}
	return NewExecutor(ch, reg, nil, nil) // ds registry + recorder unused here
}

func TestRun_RewritesPlaceholdersAndComposesCTEs(t *testing.T) {
	raw := `NODE daily_activity
SQL >
    SELECT user_id, count() events FROM events
    WHERE timestamp >= {{DateTime(start_date)}}
    GROUP BY user_id

NODE endpoint
SQL >
    SELECT * FROM daily_activity WHERE user_id = {{String(user_id)}}
TYPE endpoint`

	ch := &fakeCH{body: []byte(`{"data":[]}`)}
	e := newExec(ch, mustParse(t, "activity", raw))

	params := url.Values{"start_date": {"2024-01-01 00:00:00"}, "user_id": {"u1"}}
	body, status, err := e.Run(context.Background(), "activity", params)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if string(body) != `{"data":[]}` {
		t.Errorf("body = %s", body)
	}

	sql := ch.gotSQL
	if !strings.Contains(sql, "WITH daily_activity AS (") {
		t.Errorf("missing CTE composition:\n%s", sql)
	}
	if !strings.Contains(sql, "{start_date:DateTime}") || !strings.Contains(sql, "{user_id:String}") {
		t.Errorf("placeholders not rewritten to {name:Type}:\n%s", sql)
	}
	if strings.Contains(sql, "{{") {
		t.Errorf("raw {{...}} template left in SQL:\n%s", sql)
	}
	if !strings.HasSuffix(strings.TrimSpace(sql), "FORMAT JSON") {
		t.Errorf("SQL must end with FORMAT JSON:\n%s", sql)
	}
	if ch.gotParams["param_start_date"] != "2024-01-01 00:00:00" || ch.gotParams["param_user_id"] != "u1" {
		t.Errorf("bound params = %+v", ch.gotParams)
	}
}

// TestRunFormat_AppendsClickHouseFormat verifies the output format selects the
// trailing ClickHouse FORMAT clause (Tinybird .csv/.ndjson), while param binding
// is unchanged. Run (the JSON shorthand) must still yield FORMAT JSON.
func TestRunFormat_AppendsClickHouseFormat(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT {{String(x)}}\nTYPE endpoint"
	cases := []struct {
		format   model.OutputFormat
		wantTail string
	}{
		{model.FormatJSON, "FORMAT JSON"},
		{model.FormatCSV, "FORMAT CSVWithNames"},
		{model.FormatNDJSON, "FORMAT JSONEachRow"},
		{model.FormatParquet, "FORMAT Parquet"},
	}
	for _, c := range cases {
		t.Run(string(c.format), func(t *testing.T) {
			ch := &fakeCH{body: []byte("ok")}
			e := newExec(ch, mustParse(t, "p", raw))
			_, status, err := e.RunFormat(context.Background(), "p", url.Values{"x": {"v"}}, c.format)
			if err != nil || status != http.StatusOK {
				t.Fatalf("RunFormat: status=%d err=%v", status, err)
			}
			if got := strings.TrimSpace(ch.gotSQL); !strings.HasSuffix(got, c.wantTail) {
				t.Errorf("SQL must end with %q:\n%s", c.wantTail, got)
			}
			if ch.gotParams["param_x"] != "v" {
				t.Errorf("bound params = %+v, want param_x=v", ch.gotParams)
			}
		})
	}
}

// --- Gap 9: copy pipes (RunCopy) ---

// TestRunCopy_InsertsSelectIntoTarget verifies the on-demand copy path: it
// composes the copy SQL (upstream node as a CTE), binds request params, prefixes
// INSERT INTO <target>, runs it over the write path, and returns a terminal job.
func TestRunCopy_InsertsSelectIntoTarget(t *testing.T) {
	raw := `NODE filtered
SQL >
    SELECT * FROM events WHERE t = {{String(kind)}}

NODE cp
SQL >
    SELECT * FROM filtered
TYPE copy
TARGET_DATASOURCE archive`

	w := &fakeWriter{}
	e := newExec(&fakeCH{body: []byte("{}")}, mustParse(t, "arch", raw)).EnableCopy(w)

	body, status, err := e.RunCopy(context.Background(), "arch", url.Values{"kind": {"click"}})
	if err != nil || status != http.StatusOK {
		t.Fatalf("RunCopy: status=%d err=%v", status, err)
	}
	if w.calls != 1 {
		t.Fatalf("writer calls = %d, want 1", w.calls)
	}
	if !strings.HasPrefix(w.gotSQL, "INSERT INTO `archive` ") {
		t.Errorf("SQL must start with INSERT INTO `archive`:\n%s", w.gotSQL)
	}
	if !strings.Contains(w.gotSQL, "WITH filtered AS (") || !strings.Contains(w.gotSQL, "{kind:String}") {
		t.Errorf("composed CTE / bound placeholder missing:\n%s", w.gotSQL)
	}
	if strings.Contains(w.gotSQL, "FORMAT ") {
		t.Errorf("a copy INSERT must not carry a FORMAT clause:\n%s", w.gotSQL)
	}
	if w.gotParams["param_kind"] != "click" {
		t.Errorf("bound params = %+v, want param_kind=click", w.gotParams)
	}
	var job map[string]any
	if err := json.Unmarshal(body, &job); err != nil {
		t.Fatalf("decode job body: %v (%s)", err, body)
	}
	if job["status"] != "done" || job["pipe_name"] != "arch" {
		t.Errorf("job = %v, want status=done pipe_name=arch", job)
	}
}

func TestRunCopy_NotACopyPipe(t *testing.T) {
	raw := "NODE e\nSQL >\n    SELECT 1\nTYPE endpoint"
	e := newExec(&fakeCH{}, mustParse(t, "p", raw)).EnableCopy(&fakeWriter{})
	_, status, err := e.RunCopy(context.Background(), "p", url.Values{})
	if status != http.StatusBadRequest || err == nil {
		t.Errorf("status=%d err=%v, want 400 for a non-copy pipe", status, err)
	}
}

func TestRunCopy_WriterErrorMapsTo400(t *testing.T) {
	raw := "NODE c\nSQL >\n    SELECT 1\nTYPE copy\nTARGET_DATASOURCE t"
	w := &fakeWriter{err: errors.New("clickhouse 400")}
	e := newExec(&fakeCH{}, mustParse(t, "p", raw)).EnableCopy(w)
	_, status, err := e.RunCopy(context.Background(), "p", url.Values{})
	if status != http.StatusBadRequest || err == nil {
		t.Errorf("status=%d err=%v, want 400 on CH write error", status, err)
	}
}

func TestRunCopy_WriterNotEnabledIs500(t *testing.T) {
	raw := "NODE c\nSQL >\n    SELECT 1\nTYPE copy\nTARGET_DATASOURCE t"
	e := newExec(&fakeCH{}, mustParse(t, "p", raw)) // EnableCopy intentionally not called
	_, status, err := e.RunCopy(context.Background(), "p", url.Values{})
	if status != http.StatusInternalServerError || err == nil {
		t.Errorf("status=%d err=%v, want 500 when copy not enabled", status, err)
	}
}

func TestRunCopy_MissingRequiredParam(t *testing.T) {
	raw := "NODE c\nSQL >\n    SELECT {{String(x)}}\nTYPE copy\nTARGET_DATASOURCE t"
	w := &fakeWriter{}
	e := newExec(&fakeCH{}, mustParse(t, "p", raw)).EnableCopy(w)
	_, status, err := e.RunCopy(context.Background(), "p", url.Values{})
	if status != http.StatusBadRequest || err == nil {
		t.Errorf("status=%d err=%v, want 400 for a missing required param", status, err)
	}
	if w.calls != 0 {
		t.Errorf("writer must not run on a missing param (calls=%d)", w.calls)
	}
}

func TestRun_MissingRequiredParam(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT {{String(user_id)}}\nTYPE endpoint"
	ch := &fakeCH{}
	e := newExec(ch, mustParse(t, "p", raw))

	_, status, err := e.Run(context.Background(), "p", url.Values{})
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", status)
	}
	if err == nil || !strings.Contains(err.Error(), "missing required parameter: user_id") {
		t.Errorf("err = %v", err)
	}
	if ch.calls != 0 {
		t.Errorf("ClickHouse must not be queried on missing param (calls=%d)", ch.calls)
	}
}

func TestRun_DefaultApplied(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT g FROM t WHERE g = {{String(genre, 'rock')}}\nTYPE endpoint"
	ch := &fakeCH{body: []byte("{}")}
	e := newExec(ch, mustParse(t, "p", raw))

	if _, status, err := e.Run(context.Background(), "p", url.Values{}); err != nil || status != http.StatusOK {
		t.Fatalf("Run: status=%d err=%v", status, err)
	}
	if ch.gotParams["param_genre"] != "rock" {
		t.Errorf("default not applied: param_genre = %q, want rock", ch.gotParams["param_genre"])
	}
}

func TestRun_CacheTTLSettings(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT 1\nTYPE endpoint\nCACHE_TTL 60"
	ch := &fakeCH{body: []byte("{}")}
	e := newExec(ch, mustParse(t, "p", raw))

	if _, _, err := e.Run(context.Background(), "p", url.Values{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ch.gotSettings["use_query_cache"] != "1" || ch.gotSettings["query_cache_ttl"] != "60" {
		t.Errorf("cache settings = %+v, want use_query_cache=1 query_cache_ttl=60", ch.gotSettings)
	}
}

func TestRun_NoCacheWhenTTLZero(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT 1\nTYPE endpoint"
	ch := &fakeCH{body: []byte("{}")}
	e := newExec(ch, mustParse(t, "p", raw))

	if _, _, err := e.Run(context.Background(), "p", url.Values{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ch.gotSettings) != 0 {
		t.Errorf("expected no settings when CACHE_TTL unset, got %+v", ch.gotSettings)
	}
}

func TestRun_PipeNotFound(t *testing.T) {
	ch := &fakeCH{}
	e := newExec(ch)
	_, status, err := e.Run(context.Background(), "nope", url.Values{})
	if status != http.StatusNotFound || err == nil {
		t.Errorf("status=%d err=%v, want 404 + error", status, err)
	}
}

func TestRun_NoEndpointIs404(t *testing.T) {
	raw := "MATERIALIZATION mv\nTARGET_TABLE t\nSQL >\n    SELECT 1"
	ch := &fakeCH{}
	e := newExec(ch, mustParse(t, "p", raw))
	_, status, err := e.Run(context.Background(), "p", url.Values{})
	if status != http.StatusNotFound || err == nil {
		t.Errorf("status=%d err=%v, want 404 for non-endpoint pipe", status, err)
	}
}

func TestRun_ClickHouseErrorMapsTo400(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT 1\nTYPE endpoint"
	ch := &fakeCH{body: []byte("CH: syntax error"), err: errors.New("clickhouse 400")}
	e := newExec(ch, mustParse(t, "p", raw))

	body, status, err := e.Run(context.Background(), "p", url.Values{})
	if status != http.StatusBadRequest || err == nil {
		t.Errorf("status=%d err=%v, want 400 + error", status, err)
	}
	if string(body) != "CH: syntax error" {
		t.Errorf("CH error body should be returned for API mapping, got %q", body)
	}
}

// --- Task 2: full parameter types ---

// TestRun_ParamTypeMapping checks each template type maps to the right
// ClickHouse {name:Type} placeholder and that valid values bind through.
func TestRun_ParamTypeMapping(t *testing.T) {
	cases := []struct {
		tmpl       string // template type, e.g. "Int64"
		value      string
		wantCHType string // expected {name:Type} type in SQL
		wantBound  string // expected bound param value (after normalization)
	}{
		{"String", "hello", "String", "hello"},
		{"Int64", "42", "Int64", "42"},
		{"Int32", "-7", "Int32", "-7"},
		{"Float64", "3.14", "Float64", "3.14"},
		{"UUID", "123e4567-e89b-12d3-a456-426614174000", "UUID", "123e4567-e89b-12d3-a456-426614174000"},
		{"Boolean", "true", "UInt8", "1"},
		{"Boolean", "0", "UInt8", "0"},
		{"DateTime", "2024-01-01 00:00:00", "DateTime", "2024-01-01 00:00:00"},
		{"Date", "2024-01-01", "Date", "2024-01-01"},
		{"DateTime64", "2024-01-01 00:00:00.123", "DateTime64", "2024-01-01 00:00:00.123"},
	}
	for _, c := range cases {
		t.Run(c.tmpl+"="+c.value, func(t *testing.T) {
			raw := "NODE endpoint\nSQL >\n    SELECT * FROM t WHERE x = {{" + c.tmpl + "(x)}}\nTYPE endpoint"
			ch := &fakeCH{body: []byte("{}")}
			e := newExec(ch, mustParse(t, "p", raw))

			_, status, err := e.Run(context.Background(), "p", url.Values{"x": {c.value}})
			if err != nil || status != http.StatusOK {
				t.Fatalf("Run: status=%d err=%v", status, err)
			}
			if !strings.Contains(ch.gotSQL, "{x:"+c.wantCHType+"}") {
				t.Errorf("SQL placeholder = %q, want {x:%s}", ch.gotSQL, c.wantCHType)
			}
			if ch.gotParams["param_x"] != c.wantBound {
				t.Errorf("bound param_x = %q, want %q", ch.gotParams["param_x"], c.wantBound)
			}
		})
	}
}

// TestRun_InvalidParamValues asserts a 400 (no CH call) for malformed values.
func TestRun_InvalidParamValues(t *testing.T) {
	cases := []struct {
		tmpl, value, wantMsg string
	}{
		{"Int64", "abc", "must be an integer"},
		{"Int32", "1.5", "must be an integer"},
		{"Float64", "ten", "must be a number"},
		{"Boolean", "maybe", "must be a boolean"},
		{"UUID", "not-a-uuid", "must be a UUID"},
		{"DateTime", "", "must not be empty"},
	}
	for _, c := range cases {
		t.Run(c.tmpl+"="+c.value, func(t *testing.T) {
			raw := "NODE endpoint\nSQL >\n    SELECT {{" + c.tmpl + "(x)}}\nTYPE endpoint"
			ch := &fakeCH{body: []byte("{}")}
			e := newExec(ch, mustParse(t, "p", raw))

			_, status, err := e.Run(context.Background(), "p", url.Values{"x": {c.value}})
			if status != http.StatusBadRequest || err == nil {
				t.Fatalf("status=%d err=%v, want 400 + error", status, err)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("err = %v, want substring %q", err, c.wantMsg)
			}
			if ch.calls != 0 {
				t.Errorf("ClickHouse must not be queried on invalid param (calls=%d)", ch.calls)
			}
		})
	}
}

// TestRun_InvalidDefaultRejected asserts defaults are validated like values.
func TestRun_InvalidDefaultRejected(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT {{Int64(n, 'oops')}}\nTYPE endpoint"
	ch := &fakeCH{body: []byte("{}")}
	e := newExec(ch, mustParse(t, "p", raw))

	_, status, err := e.Run(context.Background(), "p", url.Values{})
	if status != http.StatusBadRequest || err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Fatalf("status=%d err=%v, want 400 for invalid default", status, err)
	}
}

// --- Task 3: StatsRecorder wiring ---

func TestRun_RecordsStatOnSuccess(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT 1\nTYPE endpoint"
	ch := &fakeCH{body: []byte(`{"data":[],"rows":3,"statistics":{"rows_read":12,"bytes_read":345}}`)}
	rec := &fakeRecorder{}
	e := NewExecutor(ch, fakeRegistry{m: map[string]*model.Pipe{"p": mustParse(t, "p", raw)}}, nil, rec)

	if _, _, err := e.Run(context.Background(), "p", url.Values{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.stats) != 1 {
		t.Fatalf("want 1 stat emitted, got %d", len(rec.stats))
	}
	s := rec.stats[0]
	if s.Pipe != "p" || s.StatusCode != http.StatusOK || s.Error != "" {
		t.Errorf("stat = %+v, want pipe=p status=200 no-error", s)
	}
	if s.ReadRows != 12 || s.ReadBytes != 345 {
		t.Errorf("stat read counters = rows %d bytes %d, want 12/345", s.ReadRows, s.ReadBytes)
	}
}

func TestRun_RecordsStatOnError(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT 1\nTYPE endpoint"
	ch := &fakeCH{body: []byte("boom"), err: errors.New("clickhouse 400")}
	rec := &fakeRecorder{}
	e := NewExecutor(ch, fakeRegistry{m: map[string]*model.Pipe{"p": mustParse(t, "p", raw)}}, nil, rec)

	if _, _, err := e.Run(context.Background(), "p", url.Values{}); err == nil {
		t.Fatal("expected error")
	}
	if len(rec.stats) != 1 || rec.stats[0].StatusCode != http.StatusBadRequest || rec.stats[0].Error == "" {
		t.Errorf("stats = %+v, want one 400 stat carrying the error", rec.stats)
	}
}

func TestRun_NilRecorderIsSafe(t *testing.T) {
	raw := "NODE endpoint\nSQL >\n    SELECT 1\nTYPE endpoint"
	ch := &fakeCH{body: []byte("{}")}
	e := NewExecutor(ch, fakeRegistry{m: map[string]*model.Pipe{"p": mustParse(t, "p", raw)}}, nil, nil)

	if _, status, err := e.Run(context.Background(), "p", url.Values{}); err != nil || status != http.StatusOK {
		t.Fatalf("Run with nil recorder: status=%d err=%v", status, err)
	}
}
