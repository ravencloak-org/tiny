// Package model is the shared contract for TinyRaven: the data types parsed
// from .datasource/.pipe files plus the interfaces that decouple the
// subsystems (gatherer, pipe executor, clickhouse, auth, api) from each other.
// Packages depend on these interfaces, never on each other's concrete types,
// so they compile and develop independently.
package model

import (
	"context"
	"encoding/json"
	"net/url"
)

// ---- Data types (parsed from project files) ----

// Column is one SCHEMA column in a .datasource file.
type Column struct {
	Name string
	Type string // ClickHouse type verbatim, e.g. "String", "DateTime", "JSON"
}

// Datasource is a parsed .datasource file. ENGINE defaults to
// "MergeTree ORDER BY tuple()" when omitted (ADR 0008); all ENGINE_* options
// are forwarded to ClickHouse verbatim.
type Datasource struct {
	Name       string            // file basename without extension
	Schema     []Column          // ordered, as written
	Engine     string            // "MergeTree" if ENGINE omitted
	EngineOpts map[string]string // ENGINE_* keys (full key incl. prefix) -> value, verbatim
	Connector  string            // CONNECTOR value, e.g. "http_api" (informational in MVP)
	Raw        string            // original file text
}

// QuarantineTable is the CH table invalid rows land in (ADR 0018).
func (d Datasource) QuarantineTable() string { return d.Name + "_quarantine" }

// ParamType is a templated query parameter type, e.g. String, DateTime, Int64.
type ParamType string

// Param is a {{Type(name, default)}} placeholder extracted from an ENDPOINT SQL
// body (ADR 0003). MVP supports value params only.
type Param struct {
	Name       string
	Type       ParamType
	Default    string
	HasDefault bool
}

// Node is a NODE block in a .pipe file — a named, reusable SQL fragment.
type Node struct {
	Name string
	SQL  string
}

// Endpoint is an ENDPOINT block (TYPE query) — a published API route.
type Endpoint struct {
	Name      string
	SQL       string
	Params    []Param // extracted from SQL, in order of appearance
	RateLimit int     // requests/sec, 0 = unset
	CacheTTL  int     // seconds; 0 = caching off (ADR 0009)
}

// Materialization is a MATERIALIZATION block — wired in Phase 3, parsed now.
type Materialization struct {
	Name        string
	TargetTable string
	SQL         string
}

// Pipe is a parsed .pipe file. A pipe has zero or more NODEs and at most one
// ENDPOINT or MATERIALIZATION.
type Pipe struct {
	Name     string
	Nodes    []Node
	Endpoint *Endpoint
	Material *Materialization
	Raw      string
}

// Token is an auth token (ADR 0005). Value is the secret bearer string; Name is
// the git-tracked identifier; Scopes gate access (e.g. "ADMIN", "READ:<pipe>").
type Token struct {
	Name   string
	Value  string
	Scopes []string
}

// HasScope reports whether the token carries scope s or ADMIN.
func (t Token) HasScope(s string) bool {
	for _, sc := range t.Scopes {
		if sc == s || sc == "ADMIN" {
			return true
		}
	}
	return false
}

// ---- Contract interfaces (implemented by the subsystem packages) ----

// CHInserter does batched native-protocol inserts (clickhouse-go/v2, TCP 9000;
// ADR 0013). The caller passes the datasource (for column order/types) and rows
// already parsed + validated against its schema, so the implementation can build
// a typed native batch (PrepareBatch).
type CHInserter interface {
	Insert(ctx context.Context, ds *Datasource, rows []map[string]any) error
}

// CHQuerier runs read-only queries over the ClickHouse HTTP interface (8123;
// ADR 0013). params are CH query parameters (param_<name> values); settings are
// extra SETTINGS (e.g. use_query_cache). Returns the response body verbatim
// (caller asks for FORMAT JSON).
type CHQuerier interface {
	Query(ctx context.Context, sql string, params, settings map[string]string) ([]byte, error)
}

// CHPinger is a liveness/readiness probe for ClickHouse (ADR 0024).
type CHPinger interface {
	Ping(ctx context.Context) error
}

// Pinger is a generic readiness probe (Redis, ClickHouse).
type Pinger interface {
	Ping(ctx context.Context) error
}

// DatasourceRegistry stores parsed datasource schemas (Redis-backed; ADR 0001).
type DatasourceRegistry interface {
	Get(ctx context.Context, name string) (*Datasource, bool, error)
	Put(ctx context.Context, ds *Datasource) error
	List(ctx context.Context) ([]*Datasource, error)
}

// PipeRegistry stores parsed pipes. In-memory with atomic swap on hot reload
// (ADR 0020).
type PipeRegistry interface {
	Get(name string) (*Pipe, bool)
	Put(p *Pipe)
	List() []*Pipe
}

// TokenStore validates and manages bearer tokens (Redis-backed; ADR 0005).
type TokenStore interface {
	// Validate returns the token for a bearer value, or ok=false if unknown.
	Validate(ctx context.Context, value string) (*Token, bool, error)
	Put(ctx context.Context, t *Token) error
}

// Ingester accepts raw event rows for a datasource, validates + quarantines per
// row, and buffers the valid ones (ADRs 0004, 0018). It never rejects the batch
// for bad rows; counts are returned for the 202 body.
type Ingester interface {
	Ingest(ctx context.Context, datasource string, rows []json.RawMessage) (successful, quarantined int, err error)
}

// PipeRunner executes a published pipe endpoint and returns the JSON response
// body. status is the HTTP status to send; err is for internal failures.
type PipeRunner interface {
	Run(ctx context.Context, name string, params url.Values) (body []byte, status int, err error)
}
