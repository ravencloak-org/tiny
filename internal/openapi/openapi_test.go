package openapi

import (
	"encoding/json"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

func TestGenerate_ValidJSONWithBaseSurface(t *testing.T) {
	out := Generate(nil)

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("Generate produced invalid JSON: %v", err)
	}
	if doc["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v, want 3.0.3", doc["openapi"])
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths missing or wrong type: %T", doc["paths"])
	}
	for _, want := range []string{
		"/v0/events", "/v0/sql", "/v0/pipes", "/v0/pipes/{name}",
		"/v0/datasources", "/v0/datasources/{name}", "/v0/metrics", "/health", "/health/ready",
	} {
		if _, ok := paths[want]; !ok {
			t.Errorf("base path %q missing from spec", want)
		}
	}
}

func TestGenerate_CopyPipeAndFormatVariants(t *testing.T) {
	endpoint := &model.Pipe{Name: "metrics", Endpoint: &model.Endpoint{Name: "metrics"}}
	cp := &model.Pipe{Name: "rollup_copy", Copy: &model.Copy{Name: "rollup_copy", TargetDatasource: "rollups"}}

	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(Generate([]*model.Pipe{endpoint, cp}), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Endpoint pipe gets a path per output format.
	for _, ext := range []string{"json", "csv", "ndjson", "parquet"} {
		if _, ok := doc.Paths["/v0/pipes/metrics."+ext]; !ok {
			t.Errorf("missing format path /v0/pipes/metrics.%s", ext)
		}
	}
	// COPY pipe gets a POST trigger, not a query path.
	if p, ok := doc.Paths["/v0/pipes/rollup_copy/copy"]; !ok {
		t.Error("missing POST /v0/pipes/rollup_copy/copy")
	} else if _, hasPost := p["post"]; !hasPost {
		t.Error("copy path must be a POST operation")
	}
	if _, ok := doc.Paths["/v0/pipes/rollup_copy.json"]; ok {
		t.Error("copy pipe must not produce a query path")
	}
}

func TestGenerate_PerPipePathAndParams(t *testing.T) {
	pipe := &model.Pipe{
		Name: "user_metrics",
		Endpoint: &model.Endpoint{
			Name: "user_metrics",
			Params: []model.Param{
				{Name: "user_id", Type: "String"},
				{Name: "limit", Type: "Int32", HasDefault: true, Default: "10"},
				{Name: "since", Type: "DateTime"},
			},
		},
	}
	// A materialization-only pipe must NOT produce a path.
	mv := &model.Pipe{Name: "rollup", Material: &model.Materialization{Name: "rollup"}}

	out := Generate([]*model.Pipe{pipe, mv})

	var doc struct {
		Paths map[string]struct {
			Get *struct {
				Parameters []struct {
					Name     string `json:"name"`
					In       string `json:"in"`
					Required bool   `json:"required"`
					Schema   struct {
						Type   string `json:"type"`
						Format string `json:"format"`
					} `json:"schema"`
				} `json:"parameters"`
			} `json:"get"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	const path = "/v0/pipes/user_metrics.json"
	pi, ok := doc.Paths[path]
	if !ok || pi.Get == nil {
		t.Fatalf("missing GET %s", path)
	}
	if _, ok := doc.Paths["/v0/pipes/rollup.json"]; ok {
		t.Error("materialization pipe must not produce an endpoint path")
	}

	got := map[string]struct {
		in, typ, format string
		required        bool
	}{}
	for _, p := range pi.Get.Parameters {
		got[p.Name] = struct {
			in, typ, format string
			required        bool
		}{p.In, p.Schema.Type, p.Schema.Format, p.Required}
	}
	if len(got) != 3 {
		t.Fatalf("want 3 params, got %d: %+v", len(got), got)
	}
	if g := got["user_id"]; g.typ != "string" || g.in != "query" || !g.required {
		t.Errorf("user_id param = %+v, want required string query param", g)
	}
	if g := got["limit"]; g.typ != "integer" || g.format != "int32" || g.required {
		t.Errorf("limit param = %+v, want optional int32 (has default)", g)
	}
	if g := got["since"]; g.typ != "string" || g.format != "date-time" || !g.required {
		t.Errorf("since param = %+v, want required date-time string", g)
	}
}
