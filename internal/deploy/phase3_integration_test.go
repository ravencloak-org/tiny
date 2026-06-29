//go:build integration

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tinyraven/tinyraven/internal/clickhouse"
)

// phase3Client dials the real ClickHouse at TR_CLICKHOUSE_HTTP (default
// http://localhost:8123) against TR_CLICKHOUSE_DB (default "default"), skipping
// if it is unreachable. DDL runs over HTTP, so no native transport is needed.
//
// Run with: go test -tags integration ./internal/deploy/...
func phase3Client(t *testing.T) (*clickhouse.Client, context.Context) {
	t.Helper()
	base := os.Getenv("TR_CLICKHOUSE_HTTP")
	if base == "" {
		base = "http://localhost:8123"
	}
	db := os.Getenv("TR_CLICKHOUSE_DB")
	if db == "" {
		db = "default"
	}
	ch, err := clickhouse.New(clickhouse.Config{HTTPURL: base, Database: db})
	if err != nil {
		t.Fatalf("clickhouse.New: %v", err)
	}
	t.Cleanup(func() { _ = ch.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := ch.Ping(ctx); err != nil {
		t.Skipf("ClickHouse unavailable at %s: %v", base, err)
	}
	return ch, ctx
}

func TestRun_BreakingShadowSwap_Integration(t *testing.T) {
	ch, ctx := phase3Client(t)
	const table = "tr_phase3_break"
	drop := func() {
		for _, n := range []string{table, table + "_quarantine", table + "_shadow"} {
			_, _ = ch.Query(ctx, "DROP TABLE IF EXISTS `"+n+"`", nil, nil)
		}
	}
	drop()
	defer drop()

	dir := t.TempDir()
	path := filepath.Join(dir, table+".datasource")
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	reg := newMemReg()

	// v1: create the table and seed one row.
	write("SCHEMA >\n    user_id String,\n    ts DateTime\n")
	if _, err := Run(ctx, dir, ch, reg, Options{}); err != nil {
		t.Fatalf("create deploy: %v", err)
	}
	if _, err := ch.Query(ctx, "INSERT INTO `"+table+"` VALUES ('alice', '2026-06-29 00:00:00')", nil, nil); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	// v2: drop user_id (breaking), keep ts (overlap, backfilled), add country.
	write("SCHEMA >\n    ts DateTime,\n    country String\n")
	r, err := Run(ctx, dir, ch, reg, Options{AllowBreaking: true})
	if err != nil {
		t.Fatalf("breaking deploy: %v", err)
	}
	if len(r.BreakingApplied) != 1 {
		t.Fatalf("BreakingApplied = %v, want one shadow swap", r.BreakingApplied)
	}

	// The seeded ts row must survive the swap (backfilled).
	body, err := ch.Query(ctx, "SELECT count() FROM `"+table+"` FORMAT TabSeparated", nil, nil)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "1" {
		t.Fatalf("row count after swap = %q, want 1 (backfill lost data)", got)
	}

	// The live schema must be the new one: ts + country, no user_id.
	cols, err := liveColumns(ctx, ch, table)
	if err != nil {
		t.Fatalf("liveColumns: %v", err)
	}
	if _, gone := cols["user_id"]; gone {
		t.Errorf("user_id should be dropped, live cols = %v", cols)
	}
	if _, ok := cols["country"]; !ok {
		t.Errorf("country should be added, live cols = %v", cols)
	}

	// The shadow table must be cleaned up after the swap.
	if sc, err := liveColumns(ctx, ch, table+"_shadow"); err != nil {
		t.Fatalf("liveColumns shadow: %v", err)
	} else if len(sc) != 0 {
		t.Errorf("shadow table should be dropped, got cols %v", sc)
	}
}

func TestRun_MaterializedView_Integration(t *testing.T) {
	ch, ctx := phase3Client(t)
	const (
		src = "tr_phase3_src"
		tgt = "tr_phase3_rollup"
		mv  = "tr_phase3_mv"
	)
	drop := func() {
		for _, n := range []string{mv, src, src + "_quarantine", tgt, tgt + "_quarantine"} {
			_, _ = ch.Query(ctx, "DROP TABLE IF EXISTS `"+n+"`", nil, nil)
		}
	}
	drop()
	defer drop()

	dir := t.TempDir()
	files := map[string]string{
		src + ".datasource": "SCHEMA >\n    ts DateTime,\n    user_id String\n",
		tgt + ".datasource": "SCHEMA >\n    d Date,\n    c UInt64\n",
		mv + ".pipe": "MATERIALIZATION " + mv + "\nTARGET_TABLE " + tgt + "\n" +
			"SQL >\n    SELECT toDate(ts) d, count() c FROM " + src + " GROUP BY d",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	reg := newMemReg()

	r, err := Run(ctx, dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if len(r.MaterializedViews) != 1 || r.MaterializedViews[0] != mv {
		t.Fatalf("MaterializedViews = %v, want [%s]", r.MaterializedViews, mv)
	}

	// Insert into the source: the incremental MV should fan the rows into target.
	if _, err := ch.Query(ctx, "INSERT INTO `"+src+"` VALUES "+
		"('2026-06-29 00:00:00','a'), ('2026-06-29 01:00:00','b')", nil, nil); err != nil {
		t.Fatalf("source insert: %v", err)
	}
	body, err := ch.Query(ctx, "SELECT sum(c) FROM `"+tgt+"` FORMAT TabSeparated", nil, nil)
	if err != nil {
		t.Fatalf("rollup query: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "2" {
		t.Fatalf("rollup sum = %q, want 2 (MV did not populate target)", got)
	}
}
