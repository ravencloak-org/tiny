//go:build integration

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tinyraven/tinyraven/internal/clickhouse"
)

// TestRun_Integration deploys against a real ClickHouse at TR_CLICKHOUSE_HTTP
// (default http://localhost:8123), exercising create -> additive ALTER ->
// clean re-diff. Skips if ClickHouse is unreachable. The metadata registry is
// in-memory (memReg), so no Redis is required.
//
// Run with: go test -tags integration ./internal/deploy/...
func TestRun_Integration(t *testing.T) {
	base := os.Getenv("TR_CLICKHOUSE_HTTP")
	if base == "" {
		base = "http://localhost:8123"
	}
	db := os.Getenv("TR_CLICKHOUSE_DB")
	if db == "" {
		db = "default" // always present; avoids depending on a provisioned tr_main
	}

	ch, err := clickhouse.New(clickhouse.Config{HTTPURL: base, Database: db})
	if err != nil {
		t.Fatalf("clickhouse.New: %v", err)
	}
	defer ch.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := ch.Ping(ctx); err != nil {
		t.Skipf("ClickHouse unavailable at %s: %v", base, err)
	}

	const table = "tr_deploy_it"
	drop := func() {
		_, _ = ch.Query(ctx, "DROP TABLE IF EXISTS `"+table+"`", nil, nil)
		_, _ = ch.Query(ctx, "DROP TABLE IF EXISTS `"+table+"_quarantine`", nil, nil)
	}
	drop()
	defer drop()

	dir := t.TempDir()
	dsPath := filepath.Join(dir, table+".datasource")
	write := func(content string) {
		if err := os.WriteFile(dsPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write datasource: %v", err)
		}
	}
	reg := newMemReg()

	// 1. First deploy: table is created.
	write("SCHEMA >\n    user_id String,\n    ts DateTime\n")
	r, err := Run(ctx, dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("create deploy: %v", err)
	}
	if len(r.Created) != 1 || r.Created[0] != table {
		t.Fatalf("Created = %v, want [%s]", r.Created, table)
	}

	// 2. Add a column: additive ALTER applied.
	write("SCHEMA >\n    user_id String,\n    ts DateTime,\n    country String\n")
	r, err = Run(ctx, dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("additive deploy: %v", err)
	}
	if len(r.AltersApplied) != 1 {
		t.Fatalf("AltersApplied = %v, want one ADD COLUMN", r.AltersApplied)
	}
	if len(r.Created) != 0 {
		t.Errorf("Created = %v, want none on second deploy", r.Created)
	}

	// 3. Re-diff with no changes: clean.
	r, err = Run(ctx, dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("clean re-diff: %v", err)
	}
	if len(r.Created) != 0 || len(r.AltersApplied) != 0 || len(r.Breaking) != 0 {
		t.Errorf("expected clean re-diff, got %+v", r)
	}
}
