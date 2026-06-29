//go:build integration

// Integration tests for the ClickHouse adapter — a real insert (native) +
// query (HTTP) round-trip. Run with:
//
//	go test -tags=integration ./internal/clickhouse/...
//
// Env: TR_TEST_CH_HTTP (default http://localhost:8123),
// TR_TEST_CH_NATIVE (default localhost:9000), TR_TEST_CH_DB (default default).
// Skips if ClickHouse is unreachable.
package clickhouse

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

func testClient(t *testing.T) *Client {
	t.Helper()
	httpURL := env("TR_TEST_CH_HTTP", "http://localhost:8123")
	native := env("TR_TEST_CH_NATIVE", "localhost:9000")
	db := env("TR_TEST_CH_DB", "default")
	c, err := New(Config{HTTPURL: httpURL, NativeAddr: native, Database: db, User: env("TR_TEST_CH_USER", "default")})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("clickhouse unreachable at %s: %v", httpURL, err)
	}
	return c
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestInsertQueryRoundTrip(t *testing.T) {
	c := testClient(t)
	defer c.Close()
	ctx := context.Background()

	const table = "tr_ch_it_events"
	ddl := "CREATE TABLE IF NOT EXISTS " + table +
		" (id UInt64, name String, ts DateTime) ENGINE = MergeTree ORDER BY id"
	if _, err := c.Query(ctx, ddl, nil, nil); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := c.Query(ctx, "TRUNCATE TABLE "+table, nil, nil); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() { _, _ = c.Query(context.Background(), "DROP TABLE IF EXISTS "+table, nil, nil) })

	ds := &model.Datasource{Name: table, Schema: []model.Column{
		{Name: "id", Type: "UInt64"}, {Name: "name", Type: "String"}, {Name: "ts", Type: "DateTime"},
	}}
	rows := []map[string]any{
		{"id": float64(1), "name": "alpha", "ts": "2026-06-29T00:00:00Z"},
		{"id": float64(2), "name": "beta", "ts": float64(1000000000)},
	}
	if err := c.Insert(ctx, ds, rows); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	body, err := c.Query(ctx, "SELECT count() FROM "+table+" FORMAT TabSeparated", nil, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "2" {
		t.Fatalf("count = %q, want 2", got)
	}
}
