package pipe

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

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
	return NewExecutor(ch, reg, nil) // ds registry unused by Run in MVP
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
