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
	err  error
}

func (f fakeDSReg) List(context.Context) ([]*model.Datasource, error) { return f.list, f.err }
func (fakeDSReg) Get(context.Context, string) (*model.Datasource, bool, error) {
	return nil, false, nil
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

// TestListDatasourcesAuth covers the ADMIN gate (ADR 0005): admin sees the list,
// a non-admin token is 403, no token is 401.
func TestListDatasourcesAuth(t *testing.T) {
	h := newDSServer(fakeDSReg{})
	cases := []struct {
		name, token string
		want        int
	}{
		{"admin ok", "adm", http.StatusOK},
		{"non-admin denied", "rd", http.StatusForbidden},
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
