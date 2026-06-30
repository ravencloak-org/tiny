package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// fakeDSReg is a model.DatasourceRegistry stub. Only List is exercised by the
// /v0/datasources handler; Get/Put satisfy the interface.
type fakeDSReg struct {
	list []*model.Datasource
	get  map[string]*model.Datasource // backs Get; nil -> always not-found
	err  error
}

func (f fakeDSReg) List(context.Context) ([]*model.Datasource, error) { return f.list, f.err }
func (f fakeDSReg) Get(_ context.Context, name string) (*model.Datasource, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	ds, ok := f.get[name]
	return ds, ok, nil
}
func (fakeDSReg) Put(context.Context, *model.Datasource) error { return nil }

func newDSServer(reg model.DatasourceRegistry) http.Handler {
	return New(Deps{
		Tokens: fakeTokens{m: map[string]*model.Token{
			"adm": {Name: "adm", Value: "adm", Scopes: []string{"ADMIN"}},
			"rd":  {Name: "rd", Value: "rd", Scopes: []string{"READ:user_metrics"}},
		}},
		RedisPing:   okPinger{},
		CHPing:      okPinger{},
		Datasources: reg,
	})
}

// TestListDatasourcesAuth covers auth on the scope-filtered list (Tinybird
// parity): admin and any authenticated token get 200 (the list is narrowed by
// READ scope, never 403'd); no token is still 401. The "rd" token holds only
// READ:user_metrics (a pipe scope), so against an empty registry it sees [].
func TestListDatasourcesAuth(t *testing.T) {
	h := newDSServer(fakeDSReg{})
	cases := []struct {
		name, token string
		want        int
	}{
		{"admin ok", "adm", http.StatusOK},
		{"non-admin filtered (200, narrowed)", "rd", http.StatusOK},
		{"no token", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v0/datasources", nil)
			if c.token != "" {
				req.Header.Set("Authorization", "Bearer "+c.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

// TestListDatasourcesScopeFilter verifies the per-token subset: a token with
// READ:events sees only events, not orders; ADMIN sees both.
func TestListDatasourcesScopeFilter(t *testing.T) {
	reg := fakeDSReg{list: []*model.Datasource{
		{Name: "events", Engine: "MergeTree"},
		{Name: "orders", Engine: "MergeTree"},
	}}
	h := New(Deps{
		Tokens: fakeTokens{m: map[string]*model.Token{
			"adm":   {Name: "adm", Value: "adm", Scopes: []string{"ADMIN"}},
			"rd_ev": {Name: "rd_ev", Value: "rd_ev", Scopes: []string{"READ:events"}},
		}},
		RedisPing:   okPinger{},
		CHPing:      okPinger{},
		Datasources: reg,
	})

	get := func(token string) dsListResp {
		req := httptest.NewRequest(http.MethodGet, "/v0/datasources", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("[%s] status = %d, want 200 (body %s)", token, rec.Code, rec.Body.String())
		}
		var got dsListResp
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("[%s] decode: %v", token, err)
		}
		return got
	}

	if names := dsNames(get("adm")); len(names) != 2 {
		t.Fatalf("admin sees %v, want both events+orders", names)
	}
	rd := dsNames(get("rd_ev"))
	if len(rd) != 1 || rd[0] != "events" {
		t.Fatalf("READ:events token sees %v, want [events] only", rd)
	}
}

func dsNames(r dsListResp) []string {
	out := make([]string, len(r.Datasources))
	for i, d := range r.Datasources {
		out[i] = d.Name
	}
	return out
}

// TestListDatasourcesBody verifies the Tinybird envelope and field mapping:
// {"datasources":[{name,engine,columns:[{name,type,nullable}]}]}, with nullable
// derived from the ClickHouse type.
func TestListDatasourcesBody(t *testing.T) {
	reg := fakeDSReg{list: []*model.Datasource{
		{
			Name:   "events",
			Engine: "MergeTree",
			Schema: []model.Column{
				{Name: "ts", Type: "DateTime"},
				{Name: "user_id", Type: "Nullable(String)"},
			},
		},
	}}
	h := newDSServer(reg)

	req := httptest.NewRequest(http.MethodGet, "/v0/datasources", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var got dsListResp
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if len(got.Datasources) != 1 {
		t.Fatalf("datasources = %d, want 1", len(got.Datasources))
	}
	ds := got.Datasources[0]
	if ds.Name != "events" {
		t.Fatalf("name = %q, want events", ds.Name)
	}
	if ds.Engine.Engine != "MergeTree" {
		t.Fatalf("engine = %q, want MergeTree", ds.Engine.Engine)
	}
	if len(ds.Columns) != 2 {
		t.Fatalf("columns = %d, want 2", len(ds.Columns))
	}
	if ds.Columns[0].Name != "ts" || ds.Columns[0].Type != "DateTime" || ds.Columns[0].Nullable {
		t.Fatalf("col[0] = %+v, want {ts DateTime false}", ds.Columns[0])
	}
	if !ds.Columns[1].Nullable {
		t.Fatalf("col[1] %q should be nullable", ds.Columns[1].Type)
	}
}

// TestListDatasourcesEmpty ensures an empty registry serializes as [] (not null),
// so clients can iterate without a nil check.
func TestListDatasourcesEmpty(t *testing.T) {
	h := newDSServer(fakeDSReg{list: nil})
	req := httptest.NewRequest(http.MethodGet, "/v0/datasources", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != `{"datasources":[]}`+"\n" {
		t.Fatalf("body = %q, want empty datasources array", body)
	}
}

// TestListDatasourcesError maps a registry failure to 500.
func TestListDatasourcesError(t *testing.T) {
	h := newDSServer(fakeDSReg{err: errors.New("redis down")})
	req := httptest.NewRequest(http.MethodGet, "/v0/datasources", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestGetDatasource covers GET /v0/datasources/{name}: admin gets the unwrapped
// datasource object (mirroring the list DTO), unknown -> 404, non-admin -> 403,
// registry error -> 500.
func TestGetDatasource(t *testing.T) {
	reg := fakeDSReg{get: map[string]*model.Datasource{
		"events": {
			Name:   "events",
			Engine: "MergeTree",
			Schema: []model.Column{
				{Name: "ts", Type: "DateTime"},
				{Name: "user_id", Type: "Nullable(String)"},
			},
		},
	}}
	h := newDSServer(reg)

	// admin, found: unwrapped dsItem with mapped columns.
	req := httptest.NewRequest(http.MethodGet, "/v0/datasources/events", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got dsItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if got.Name != "events" || got.Engine.Engine != "MergeTree" || len(got.Columns) != 2 {
		t.Fatalf("ds = %+v, want events/MergeTree/2 cols", got)
	}
	if !got.Columns[1].Nullable {
		t.Fatalf("col[1] %q should be nullable", got.Columns[1].Type)
	}

	// unknown -> 404
	req = httptest.NewRequest(http.MethodGet, "/v0/datasources/missing", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing ds status = %d, want 404", rec.Code)
	}

	// non-admin -> 403
	req = httptest.NewRequest(http.MethodGet, "/v0/datasources/events", nil)
	req.Header.Set("Authorization", "Bearer rd")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", rec.Code)
	}

	// registry error -> 500
	herr := newDSServer(fakeDSReg{err: errors.New("redis down")})
	req = httptest.NewRequest(http.MethodGet, "/v0/datasources/events", nil)
	req.Header.Set("Authorization", "Bearer adm")
	rec = httptest.NewRecorder()
	herr.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("error status = %d, want 500", rec.Code)
	}
}
