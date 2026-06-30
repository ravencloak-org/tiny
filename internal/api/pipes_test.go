package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// fmtRunner is a model.PipeRunner that records the output format it was asked
// for and returns a canned body — exercises the format-negotiation routes.
type fmtRunner struct {
	gotFormat model.OutputFormat
	body      []byte
}

func (f *fmtRunner) Run(ctx context.Context, name string, p url.Values) ([]byte, int, error) {
	return f.RunFormat(ctx, name, p, model.FormatJSON)
}
func (f *fmtRunner) RunFormat(_ context.Context, _ string, _ url.Values, format model.OutputFormat) ([]byte, int, error) {
	f.gotFormat = format
	return f.body, http.StatusOK, nil
}

// fakePipeReg is an in-memory model.PipeRegistry for the metadata handlers.
type fakePipeReg struct{ m map[string]*model.Pipe }

func (r fakePipeReg) Get(name string) (*model.Pipe, bool) { p, ok := r.m[name]; return p, ok }
func (fakePipeReg) Put(*model.Pipe)                       {}
func (r fakePipeReg) List() []*model.Pipe {
	out := make([]*model.Pipe, 0, len(r.m))
	for _, p := range r.m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func adminTokens() fakeTokens {
	return fakeTokens{m: map[string]*model.Token{
		"adm": {Name: "adm", Value: "adm", Scopes: []string{"ADMIN"}},
		"rd":  {Name: "rd", Value: "rd", Scopes: []string{"READ:user_metrics"}},
	}}
}

func samplePipe() *model.Pipe {
	return &model.Pipe{
		Name:  "user_metrics",
		Nodes: []model.Node{{Name: "base", SQL: "SELECT 1"}},
		Endpoint: &model.Endpoint{
			Name: "user_metrics",
			SQL:  "SELECT * FROM base WHERE id = {{String(id)}} LIMIT {{Int32(lim, 10)}}",
			Params: []model.Param{
				{Name: "id", Type: "String"},
				{Name: "lim", Type: "Int32", Default: "10", HasDefault: true},
			},
		},
	}
}

// TestPipeFormatNegotiation verifies each .{format} route maps to the right
// output format + content type (json/csv/ndjson), matching Tinybird.
func TestPipeFormatNegotiation(t *testing.T) {
	cases := []struct {
		path, contentType string
		format            model.OutputFormat
	}{
		{"/v0/pipes/user_metrics.json", "application/json", model.FormatJSON},
		{"/v0/pipes/user_metrics.csv", "text/csv", model.FormatCSV},
		{"/v0/pipes/user_metrics.ndjson", "application/x-ndjson", model.FormatNDJSON},
	}
	for _, c := range cases {
		t.Run(string(c.format), func(t *testing.T) {
			runner := &fmtRunner{body: []byte("a,b\n1,2\n")}
			h := New(Deps{
				Pipes:     runner,
				Tokens:    adminTokens(),
				RedisPing: okPinger{},
				CHPing:    okPinger{},
			})
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			req.Header.Set("Authorization", "Bearer adm")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if runner.gotFormat != c.format {
				t.Fatalf("runner format = %q, want %q", runner.gotFormat, c.format)
			}
			if ct := rec.Header().Get("Content-Type"); ct != c.contentType {
				t.Fatalf("content-type = %q, want %q", ct, c.contentType)
			}
			if rec.Body.String() != "a,b\n1,2\n" {
				t.Fatalf("body = %q, want verbatim runner output", rec.Body.String())
			}
		})
	}
}

// TestPipeFormatScope confirms the data routes stay READ-scoped (not ADMIN):
// a READ:<pipe> token may query .csv; a wrong-pipe READ token is 403.
func TestPipeFormatScope(t *testing.T) {
	h := New(Deps{
		Pipes:     &fmtRunner{body: []byte("x\n")},
		Tokens:    adminTokens(),
		RedisPing: okPinger{},
		CHPing:    okPinger{},
	})
	cases := []struct {
		token, path string
		want        int
	}{
		{"rd", "/v0/pipes/user_metrics.csv", http.StatusOK},
		{"rd", "/v0/pipes/other.csv", http.StatusForbidden},
		{"", "/v0/pipes/user_metrics.csv", http.StatusUnauthorized},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Fatalf("%s [%s] = %d, want %d", c.path, c.token, rec.Code, c.want)
		}
	}
}

func newPipeServer(reg model.PipeRegistry) http.Handler {
	return New(Deps{
		Pipes:     &fmtRunner{body: []byte("{}")},
		PipeReg:   reg,
		Tokens:    adminTokens(),
		RedisPing: okPinger{},
		CHPing:    okPinger{},
	})
}

// TestListPipes verifies the {"pipes":[...]} envelope and the node/param DTO.
func TestListPipes(t *testing.T) {
	h := newPipeServer(fakePipeReg{m: map[string]*model.Pipe{"user_metrics": samplePipe()}})
	req := httptest.NewRequest(http.MethodGet, "/v0/pipes", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got pipeListResp
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if len(got.Pipes) != 1 {
		t.Fatalf("pipes = %d, want 1", len(got.Pipes))
	}
	assertSamplePipe(t, got.Pipes[0])
}

// TestGetPipe verifies the single (unwrapped) pipe object, plus 404/auth.
func TestGetPipe(t *testing.T) {
	h := newPipeServer(fakePipeReg{m: map[string]*model.Pipe{"user_metrics": samplePipe()}})

	req := httptest.NewRequest(http.MethodGet, "/v0/pipes/user_metrics", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got pipeItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	assertSamplePipe(t, got)

	// unknown -> 404
	req = httptest.NewRequest(http.MethodGet, "/v0/pipes/missing", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing pipe status = %d, want 404", rec.Code)
	}
}

// TestPipeMetaAuth: metadata routes are scope-filtered (Tinybird parity), not
// wholesale ADMIN-gated. The list narrows to READ-able pipes (never 403s an
// authenticated token); a single GET requires READ for that pipe (403 if not),
// and an unauthenticated request is 401. "rd" holds READ:user_metrics.
func TestPipeMetaAuth(t *testing.T) {
	h := newPipeServer(fakePipeReg{m: map[string]*model.Pipe{"user_metrics": samplePipe()}})
	cases := []struct {
		name, token, path string
		want              int
	}{
		{"list scoped token ok (narrowed)", "rd", "/v0/pipes", http.StatusOK},
		{"list no token", "", "/v0/pipes", http.StatusUnauthorized},
		{"get readable pipe ok", "rd", "/v0/pipes/user_metrics", http.StatusOK},
		{"get unreadable pipe 403", "rd", "/v0/pipes/other", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("%s = %d, want %d (body %s)", c.path, rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestListPipesScopeFilter verifies the per-token subset: a READ:user_metrics
// token sees only user_metrics, not other_pipe; ADMIN sees both.
func TestListPipesScopeFilter(t *testing.T) {
	other := &model.Pipe{
		Name:     "other_pipe",
		Endpoint: &model.Endpoint{Name: "other_pipe", SQL: "SELECT 1"},
	}
	h := newPipeServer(fakePipeReg{m: map[string]*model.Pipe{
		"user_metrics": samplePipe(),
		"other_pipe":   other,
	}})

	get := func(token string) pipeListResp {
		req := httptest.NewRequest(http.MethodGet, "/v0/pipes", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("[%s] status = %d, want 200 (body %s)", token, rec.Code, rec.Body.String())
		}
		var got pipeListResp
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("[%s] decode: %v", token, err)
		}
		return got
	}

	if got := get("adm"); len(got.Pipes) != 2 {
		t.Fatalf("admin sees %d pipes, want 2", len(got.Pipes))
	}
	rd := get("rd")
	if len(rd.Pipes) != 1 || rd.Pipes[0].Name != "user_metrics" {
		t.Fatalf("READ:user_metrics token sees %d pipes, want [user_metrics]", len(rd.Pipes))
	}
}

// TestListPipesEmpty: empty registry serializes as [] (not null).
func TestListPipesEmpty(t *testing.T) {
	h := newPipeServer(fakePipeReg{m: map[string]*model.Pipe{}})
	req := httptest.NewRequest(http.MethodGet, "/v0/pipes", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != `{"pipes":[]}`+"\n" {
		t.Fatalf("body = %q, want empty pipes array", body)
	}
}

// assertSamplePipe checks the DTO mapping of samplePipe(): type=endpoint, the
// endpoint name surfaced, upstream node has empty params, and the endpoint node
// carries both params with default null/value respectively.
func assertSamplePipe(t *testing.T, p pipeItem) {
	t.Helper()
	if p.Name != "user_metrics" || p.Type != "endpoint" {
		t.Fatalf("pipe = {%q %q}, want {user_metrics endpoint}", p.Name, p.Type)
	}
	if p.Endpoint == nil || *p.Endpoint != "user_metrics" {
		t.Fatalf("endpoint = %v, want user_metrics", p.Endpoint)
	}
	if len(p.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(p.Nodes))
	}
	if p.Nodes[0].Name != "base" || len(p.Nodes[0].Params) != 0 {
		t.Fatalf("node[0] = %+v, want base with [] params", p.Nodes[0])
	}
	ep := p.Nodes[1]
	if ep.Name != "user_metrics" || len(ep.Params) != 2 {
		t.Fatalf("endpoint node = %+v, want user_metrics with 2 params", ep)
	}
	if ep.Params[0].Name != "id" || ep.Params[0].Type != "String" || ep.Params[0].Default != nil {
		t.Fatalf("param[0] = %+v, want id/String/null default", ep.Params[0])
	}
	if ep.Params[1].Default == nil || *ep.Params[1].Default != "10" {
		t.Fatalf("param[1] default = %v, want \"10\"", ep.Params[1].Default)
	}
}
