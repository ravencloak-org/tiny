package datasource

import (
	"strings"
	"testing"
)

func TestParse_SchemaOrderingAndEngine(t *testing.T) {
	raw := `SCHEMA >
    event_id String,
    user_id String,
    timestamp DateTime,
    properties JSON
ENGINE "MergeTree"
ENGINE_SORTING_KEY "(user_id, timestamp)"
ENGINE_PARTITION_KEY "toYYYYMM(timestamp)"
ENGINE_TTL "timestamp + interval 90 day"
CONNECTOR http_api`

	ds, err := Parse("events", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantCols := []struct{ name, typ string }{
		{"event_id", "String"},
		{"user_id", "String"},
		{"timestamp", "DateTime"},
		{"properties", "JSON"},
	}
	if len(ds.Schema) != len(wantCols) {
		t.Fatalf("got %d columns, want %d: %+v", len(ds.Schema), len(wantCols), ds.Schema)
	}
	for i, w := range wantCols {
		if ds.Schema[i].Name != w.name || ds.Schema[i].Type != w.typ {
			t.Errorf("col[%d] = %+v, want {%s %s}", i, ds.Schema[i], w.name, w.typ)
		}
	}
	if ds.Engine != "MergeTree" {
		t.Errorf("Engine = %q, want MergeTree", ds.Engine)
	}
	if got := ds.EngineOpts["ENGINE_SORTING_KEY"]; got != "(user_id, timestamp)" {
		t.Errorf("ENGINE_SORTING_KEY = %q (kept verbatim, unquoted)", got)
	}
	if got := ds.EngineOpts["ENGINE_TTL"]; got != "timestamp + interval 90 day" {
		t.Errorf("ENGINE_TTL = %q", got)
	}
	if ds.Connector != "http_api" {
		t.Errorf("Connector = %q, want http_api", ds.Connector)
	}
	if ds.Raw != raw {
		t.Errorf("Raw not preserved verbatim")
	}
	if ds.QuarantineTable() != "events_quarantine" {
		t.Errorf("QuarantineTable = %q", ds.QuarantineTable())
	}
}

func TestParse_EngineDefault(t *testing.T) {
	ds, err := Parse("d", "SCHEMA >\n    a String\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if ds.Engine != "MergeTree" {
		t.Errorf("Engine = %q, want MergeTree default", ds.Engine)
	}
}

func TestParse_JSONPathStripped(t *testing.T) {
	raw := "SCHEMA >\n" +
		"    user_id String `json:$.user.id`,\n" +
		"    tags Map(String, String),\n" +
		"    ts DateTime json:$.ts\n"
	ds, err := Parse("d", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := map[string]string{
		"user_id": "String",
		"tags":    "Map(String, String)", // spaced CH type preserved
		"ts":      "DateTime",
	}
	for _, c := range ds.Schema {
		if want[c.Name] != c.Type {
			t.Errorf("col %s type = %q, want %q", c.Name, c.Type, want[c.Name])
		}
	}
}

func TestParse_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string // substring expected in error; "" means no error
	}{
		{
			name:    "missing schema",
			raw:     `ENGINE "MergeTree"`,
			wantErr: "SCHEMA is required",
		},
		{
			name: "sorting key references unknown column",
			raw: `SCHEMA >
    a String
ENGINE_SORTING_KEY "(a, missing_col)"`,
			wantErr: `references unknown column "missing_col"`,
		},
		{
			name: "function and interval keywords are not treated as columns",
			raw: `SCHEMA >
    timestamp DateTime
ENGINE_PARTITION_KEY "toYYYYMM(timestamp)"
ENGINE_TTL "timestamp + interval 90 day"`,
			wantErr: "",
		},
		{
			name: "valid multi-column sorting key",
			raw: `SCHEMA >
    user_id String,
    timestamp DateTime
ENGINE_SORTING_KEY "(user_id, timestamp)"`,
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("d", tt.raw)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParse_EngineOptsAreUppercasedAndVerbatim(t *testing.T) {
	raw := `SCHEMA >
    a String
ENGINE_VER "ver_col"`
	ds, err := Parse("d", raw)
	// ENGINE_VER is not one of the validated expr keys, so no referential check.
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, ok := ds.EngineOpts["ENGINE_VER"]; !ok || got != "ver_col" {
		t.Errorf("ENGINE_VER = %q ok=%v, want ver_col", got, ok)
	}
}
