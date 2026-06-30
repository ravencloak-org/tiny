// Package openapi emits an OpenAPI 3.0 spec for a TinyRaven deployment from the
// live pipe registry (ADR 0017). It only ever marshals a fixed, known shape — a
// static /v0 base plus one path per published endpoint pipe — so it owns a
// minimal set of OpenAPI-3 structs and uses stdlib encoding/json; it never
// parses or validates third-party specs, so kin-openapi is intentionally not a
// dependency (ADR 0032).
package openapi

import (
	"encoding/json"

	"github.com/tinyraven/tinyraven/internal/model"
)

// ---- Minimal OpenAPI 3.0 structs (only the subset we emit) ----

type document struct {
	OpenAPI string              `json:"openapi"`
	Info    info                `json:"info"`
	Paths   map[string]pathItem `json:"paths"`
}

type info struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type pathItem struct {
	Get  *operation `json:"get,omitempty"`
	Post *operation `json:"post,omitempty"`
}

type operation struct {
	Summary     string              `json:"summary,omitempty"`
	OperationID string              `json:"operationId,omitempty"`
	Parameters  []parameter         `json:"parameters,omitempty"`
	Responses   map[string]response `json:"responses"`
}

type parameter struct {
	Name     string `json:"name"`
	In       string `json:"in"`
	Required bool   `json:"required"`
	Schema   schema `json:"schema"`
}

type schema struct {
	Type       string            `json:"type,omitempty"`
	Format     string            `json:"format,omitempty"`
	Items      *schema           `json:"items,omitempty"`
	Properties map[string]schema `json:"properties,omitempty"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

type mediaType struct {
	Schema schema `json:"schema"`
}

// Generate returns the OpenAPI 3.0 JSON describing the frozen /v0 surface plus a
// GET /v0/pipes/{name}.json path for every endpoint pipe, with each pipe's
// {{Type(name)}} template params rendered as typed query parameters (ADR 0017).
// The /v0 surface is frozen and the native extension surface is namespaced (ADR
// 0029).
func Generate(pipes []*model.Pipe) []byte {
	doc := document{
		OpenAPI: "3.0.3",
		Info:    info{Title: "TinyRaven API", Version: "v0"},
		Paths:   basePaths(),
	}
	for _, p := range pipes {
		// COPY pipes (TYPE copy) publish an on-demand trigger, not a query route.
		if p.Copy != nil {
			doc.Paths["/v0/pipes/"+p.Name+"/copy"] = pathItem{
				Post: &operation{
					Summary:     "Run the " + p.Name + " copy pipe (INSERT INTO " + p.Copy.TargetDatasource + ")",
					OperationID: "copyPipe_" + p.Name,
					Responses:   map[string]response{"200": {Description: "Copy executed"}},
				},
			}
			continue
		}
		if p.Endpoint == nil {
			continue // materializations publish no HTTP route
		}
		// One path per output format (ADR 0029 parity): .json carries the typed
		// JSON envelope; .csv/.ndjson/.parquet are the same query in other formats.
		params := pipeParams(p.Endpoint.Params)
		doc.Paths["/v0/pipes/"+p.Name+".json"] = pathItem{Get: &operation{
			Summary: "Query the " + p.Name + " pipe (JSON)", OperationID: "queryPipe_" + p.Name,
			Parameters: params, Responses: map[string]response{"200": pipeResponse()},
		}}
		for _, f := range []struct{ ext, ctype, desc string }{
			{"csv", "text/csv", "CSV with header row"},
			{"ndjson", "application/x-ndjson", "Newline-delimited JSON rows"},
			{"parquet", "application/octet-stream", "Apache Parquet (binary)"},
		} {
			doc.Paths["/v0/pipes/"+p.Name+"."+f.ext] = pathItem{Get: &operation{
				Summary:     "Query the " + p.Name + " pipe (" + f.ext + ")",
				OperationID: "queryPipe_" + p.Name + "_" + f.ext,
				Parameters:  params,
				Responses:   map[string]response{"200": {Description: f.desc, Content: map[string]mediaType{f.ctype: {Schema: schema{Type: "string"}}}}},
			}}
		}
	}
	// MarshalIndent: the spec is served to humans and tooling; the small size
	// makes pretty-printing worthwhile. encoding/json sorts map keys, so output
	// is deterministic.
	out, _ := json.MarshalIndent(doc, "", "  ")
	return out
}

// basePaths is the static, frozen /v0 surface (ADR 0029): events ingestion, the
// SQL endpoint, pipe listing, health probes, and the Prometheus metrics scrape.
func basePaths() map[string]pathItem {
	jsonResp := func(desc string) map[string]response {
		return map[string]response{"200": {Description: desc}}
	}
	return map[string]pathItem{
		"/v0/events": {Post: &operation{
			Summary:     "Ingest events into a datasource",
			OperationID: "postEvents",
			Parameters: []parameter{
				{Name: "name", In: "query", Required: true, Schema: schema{Type: "string"}},
			},
			Responses: map[string]response{"202": {Description: "Events accepted (buffered)"}},
		}},
		"/v0/sql": {Post: &operation{
			Summary:     "Run an ad-hoc SQL query",
			OperationID: "postSQL",
			Responses:   jsonResp("Query result"),
		}},
		"/v0/pipes": {Get: &operation{
			Summary:     "List published pipes (scope-filtered; ADMIN sees all)",
			OperationID: "listPipes",
			Responses:   jsonResp("Pipe list"),
		}},
		"/v0/pipes/{name}": {Get: &operation{
			Summary:     "Get a pipe definition (nodes, endpoint params)",
			OperationID: "getPipe",
			Parameters:  []parameter{nameParam()},
			Responses:   jsonResp("Pipe definition"),
		}},
		"/v0/datasources": {Get: &operation{
			Summary:     "List datasources (scope-filtered; ADMIN sees all)",
			OperationID: "listDatasources",
			Responses:   jsonResp("Datasource list"),
		}},
		"/v0/datasources/{name}": {Get: &operation{
			Summary:     "Get a datasource (schema + engine)",
			OperationID: "getDatasource",
			Parameters:  []parameter{nameParam()},
			Responses:   jsonResp("Datasource detail"),
		}},
		"/v0/metrics": {Get: &operation{
			Summary:     "Prometheus metrics scrape",
			OperationID: "getMetrics",
			Responses:   jsonResp("Prometheus exposition text"),
		}},
		"/health": {Get: &operation{
			Summary:     "Liveness probe",
			OperationID: "getHealth",
			Responses:   jsonResp("Service is live"),
		}},
		"/health/ready": {Get: &operation{
			Summary:     "Readiness probe (Redis + ClickHouse)",
			OperationID: "getHealthReady",
			Responses:   jsonResp("Service is ready"),
		}},
	}
}

// pipeParams renders endpoint template params as typed query parameters. A param
// is required iff it has no default.
// nameParam is the {name} path parameter shared by the per-resource GET routes.
func nameParam() parameter {
	return parameter{Name: "name", In: "path", Required: true, Schema: schema{Type: "string"}}
}

func pipeParams(params []model.Param) []parameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]parameter, 0, len(params))
	for _, p := range params {
		out = append(out, parameter{
			Name:     p.Name,
			In:       "query",
			Required: !p.HasDefault,
			Schema:   paramSchema(p.Type),
		})
	}
	return out
}

// paramSchema maps a template parameter type to an OpenAPI schema (best-effort).
func paramSchema(pt model.ParamType) schema {
	switch pt {
	case "Int64":
		return schema{Type: "integer", Format: "int64"}
	case "Int32":
		return schema{Type: "integer", Format: "int32"}
	case "Float64":
		return schema{Type: "number", Format: "double"}
	case "Boolean":
		return schema{Type: "boolean"}
	case "UUID":
		return schema{Type: "string", Format: "uuid"}
	case "DateTime", "DateTime64":
		return schema{Type: "string", Format: "date-time"}
	case "Date":
		return schema{Type: "string", Format: "date"}
	default: // String and unlisted types
		return schema{Type: "string"}
	}
}

// pipeResponse is the best-effort 200 schema: ClickHouse's FORMAT JSON envelope
// with meta + data arrays (ADR 0017 — best-effort, not column-precise).
func pipeResponse() response {
	return response{
		Description: "Query result",
		Content: map[string]mediaType{
			"application/json": {Schema: schema{
				Type: "object",
				Properties: map[string]schema{
					"meta": {Type: "array", Items: &schema{Type: "object"}},
					"data": {Type: "array", Items: &schema{Type: "object"}},
				},
			}},
		},
	}
}
