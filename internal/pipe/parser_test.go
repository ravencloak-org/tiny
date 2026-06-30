package pipe

import (
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

func TestParse_NodeAndEndpoint(t *testing.T) {
	raw := `NODE daily_activity
SQL >
    SELECT toDate(timestamp) date, user_id, count() events
    FROM events WHERE timestamp >= {{DateTime(start_date)}}
    GROUP BY date, user_id

NODE endpoint
SQL >
    SELECT * FROM daily_activity WHERE user_id = {{String(user_id)}}
TYPE endpoint
RATE_LIMIT 100
CACHE_TTL 60`

	p, err := Parse("activity", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Name != "activity" {
		t.Errorf("Name = %q", p.Name)
	}
	// daily_activity is upstream (CTE) node; endpoint is terminal -> not a node.
	if len(p.Nodes) != 1 || p.Nodes[0].Name != "daily_activity" {
		t.Fatalf("Nodes = %+v, want [daily_activity]", p.Nodes)
	}
	if !strings.Contains(p.Nodes[0].SQL, "GROUP BY date, user_id") {
		t.Errorf("node SQL not captured: %q", p.Nodes[0].SQL)
	}
	if p.Endpoint == nil {
		t.Fatal("Endpoint is nil")
	}
	if p.Endpoint.Name != "endpoint" {
		t.Errorf("Endpoint.Name = %q", p.Endpoint.Name)
	}
	if p.Endpoint.RateLimit != 100 || p.Endpoint.CacheTTL != 60 {
		t.Errorf("RateLimit=%d CacheTTL=%d, want 100/60", p.Endpoint.RateLimit, p.Endpoint.CacheTTL)
	}
	// Params aggregate node + endpoint placeholders, first-seen order.
	wantParams := []model.Param{
		{Name: "start_date", Type: "DateTime"},
		{Name: "user_id", Type: "String"},
	}
	if len(p.Endpoint.Params) != len(wantParams) {
		t.Fatalf("Params = %+v, want %+v", p.Endpoint.Params, wantParams)
	}
	for i, w := range wantParams {
		got := p.Endpoint.Params[i]
		if got.Name != w.Name || got.Type != w.Type || got.HasDefault {
			t.Errorf("param[%d] = %+v, want %+v (no default)", i, got, w)
		}
	}
}

func TestParse_CopyPipe(t *testing.T) {
	raw := `NODE filtered
SQL >
    SELECT * FROM events WHERE event = {{String(event_type)}}

NODE cp
SQL >
    SELECT * FROM filtered
TYPE copy
TARGET_DATASOURCE events_archive
COPY_SCHEDULE 0 * * * *`

	p, err := Parse("archive_events", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Endpoint != nil || p.Material != nil {
		t.Fatalf("copy pipe must not parse as endpoint/materialization")
	}
	if p.Copy == nil {
		t.Fatal("Copy is nil")
	}
	if p.Copy.TargetDatasource != "events_archive" {
		t.Errorf("TargetDatasource = %q, want events_archive", p.Copy.TargetDatasource)
	}
	if p.Copy.Schedule != "0 * * * *" {
		t.Errorf("Schedule = %q, want \"0 * * * *\"", p.Copy.Schedule)
	}
	// Upstream "filtered" becomes a CTE node; "cp" is the terminal copy block.
	if len(p.Nodes) != 1 || p.Nodes[0].Name != "filtered" {
		t.Fatalf("Nodes = %+v, want [filtered]", p.Nodes)
	}
	// The upstream node's placeholder is aggregated onto the copy's params.
	if len(p.Copy.Params) != 1 || p.Copy.Params[0].Name != "event_type" || p.Copy.Params[0].Type != "String" {
		t.Fatalf("Copy.Params = %+v, want [event_type:String]", p.Copy.Params)
	}
}

func TestParse_CopyMissingTargetFails(t *testing.T) {
	raw := "NODE c\nSQL >\n    SELECT 1\nTYPE copy"
	if _, err := Parse("p", raw); err == nil || !strings.Contains(err.Error(), "TARGET_DATASOURCE") {
		t.Fatalf("err = %v, want a TARGET_DATASOURCE requirement error", err)
	}
}

func TestParse_ParamDefaultsAndDedup(t *testing.T) {
	raw := `NODE endpoint
SQL >
    SELECT * FROM t
    WHERE g = {{String(genre, 'rock')}}
      AND lim = {{Int32(limit, 10)}}
      AND g2 = {{String(genre)}}
TYPE endpoint`

	p, err := Parse("p", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	params := p.Endpoint.Params
	if len(params) != 2 {
		t.Fatalf("want 2 deduped params, got %+v", params)
	}
	if params[0].Name != "genre" || !params[0].HasDefault || params[0].Default != "rock" {
		t.Errorf("genre param = %+v, want default rock (quotes stripped)", params[0])
	}
	if params[1].Name != "limit" || !params[1].HasDefault || params[1].Default != "10" {
		t.Errorf("limit param = %+v, want default 10", params[1])
	}
}

func TestParse_InlineSQLAndDefaultTerminal(t *testing.T) {
	// No TYPE marker: the last node is the terminal endpoint. Inline SQL form.
	raw := `NODE a
SQL > SELECT 1 AS x

NODE b
SQL >
    SELECT * FROM a`

	p, err := Parse("p", raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(p.Nodes) != 1 || p.Nodes[0].Name != "a" || p.Nodes[0].SQL != "SELECT 1 AS x" {
		t.Fatalf("Nodes = %+v, want [a: SELECT 1 AS x]", p.Nodes)
	}
	if p.Endpoint == nil || p.Endpoint.Name != "b" {
		t.Fatalf("Endpoint = %+v, want terminal node b", p.Endpoint)
	}
}

func TestParse_EndpointHeaderAndMaterialization(t *testing.T) {
	t.Run("endpoint header", func(t *testing.T) {
		raw := `ENDPOINT top_users
SQL >
    SELECT user_id FROM events LIMIT {{Int32(n, 5)}}`
		p, err := Parse("p", raw)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if len(p.Nodes) != 0 {
			t.Errorf("Nodes = %+v, want empty", p.Nodes)
		}
		if p.Endpoint == nil || p.Endpoint.Name != "top_users" {
			t.Fatalf("Endpoint = %+v", p.Endpoint)
		}
		if len(p.Endpoint.Params) != 1 || p.Endpoint.Params[0].Name != "n" {
			t.Errorf("Params = %+v", p.Endpoint.Params)
		}
	})

	t.Run("materialization header", func(t *testing.T) {
		raw := `MATERIALIZATION mv_daily
TARGET_TABLE daily_rollup
SQL >
    SELECT toDate(timestamp) d, count() c FROM events GROUP BY d`
		p, err := Parse("p", raw)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if p.Endpoint != nil {
			t.Errorf("Endpoint should be nil for materialization, got %+v", p.Endpoint)
		}
		if p.Material == nil || p.Material.Name != "mv_daily" || p.Material.TargetTable != "daily_rollup" {
			t.Fatalf("Material = %+v", p.Material)
		}
	})
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name, raw, wantErr string
	}{
		{"empty", "", "no NODE/ENDPOINT"},
		{"node missing name", "NODE\nSQL > SELECT 1", "missing a name"},
		{"sql before block", "SQL > SELECT 1", "before any NODE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("p", tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
