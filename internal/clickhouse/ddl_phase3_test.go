package clickhouse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// capture records the body and ?database= of the most recent HTTP request, so
// the DDL-builder tests can assert the exact SQL emitted without a real
// ClickHouse.
type capture struct {
	body string
	db   string
}

func newCaptureClient(t *testing.T) (*Client, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.body = string(b)
		cap.db = r.URL.Query().Get("database")
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{HTTPURL: srv.URL, Database: "tr_main", User: "default"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, cap
}

func TestCreateDatabaseDDL(t *testing.T) {
	c, cap := newCaptureClient(t)
	if err := c.CreateDatabase(context.Background(), "tr_feature-x"); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	if want := "CREATE DATABASE IF NOT EXISTS `tr_feature-x`"; cap.body != want {
		t.Errorf("DDL = %q, want %q", cap.body, want)
	}
	if err := c.CreateDatabase(context.Background(), ""); err == nil {
		t.Error("empty name should error")
	}
}

func TestCreateMaterializedViewDDL(t *testing.T) {
	c, cap := newCaptureClient(t)
	m := &model.Materialization{Name: "mv_daily", TargetTable: "daily_rollup", SQL: "SELECT 1 AS c"}
	if err := c.CreateMaterializedView(context.Background(), m); err != nil {
		t.Fatalf("CreateMaterializedView: %v", err)
	}
	want := "CREATE MATERIALIZED VIEW IF NOT EXISTS `mv_daily` TO `daily_rollup` AS SELECT 1 AS c"
	if cap.body != want {
		t.Errorf("DDL = %q, want %q", cap.body, want)
	}

	// Validation: a missing target / name / SQL must error before any request.
	for _, bad := range []*model.Materialization{
		{Name: "mv", SQL: "SELECT 1"},                // no target
		{TargetTable: "t", SQL: "SELECT 1"},          // no name
		{Name: "mv", TargetTable: "t", SQL: "   \n"}, // empty SQL
	} {
		cap.body = ""
		if err := c.CreateMaterializedView(context.Background(), bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
		if cap.body != "" {
			t.Errorf("no request should be sent for invalid MV %+v, got %q", bad, cap.body)
		}
	}
}

func TestExchangeTablesDDL(t *testing.T) {
	c, cap := newCaptureClient(t)
	if err := c.ExchangeTables(context.Background(), "events", "events_shadow"); err != nil {
		t.Fatalf("ExchangeTables: %v", err)
	}
	if want := "EXCHANGE TABLES `events` AND `events_shadow`"; cap.body != want {
		t.Errorf("DDL = %q, want %q", cap.body, want)
	}
	if err := c.ExchangeTables(context.Background(), "a", ""); err == nil {
		t.Error("empty table name should error")
	}
}

func TestCreateShadowTableDDL(t *testing.T) {
	c, cap := newCaptureClient(t)
	ds := &model.Datasource{
		Name:       "events",
		Schema:     []model.Column{{Name: "user_id", Type: "String"}, {Name: "ts", Type: "DateTime"}},
		EngineOpts: map[string]string{"ENGINE_SORTING_KEY": "ts"},
	}
	if err := c.CreateShadowTable(context.Background(), ds, "events_shadow"); err != nil {
		t.Fatalf("CreateShadowTable: %v", err)
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS `events_shadow`",
		"`user_id` String",
		"`ts` DateTime",
		"ENGINE = MergeTree",
		"ORDER BY ts",
	} {
		if !strings.Contains(cap.body, want) {
			t.Errorf("shadow DDL missing %q; got:\n%s", want, cap.body)
		}
	}
	// The shadow carries the data table only, never the quarantine sibling.
	if strings.Contains(cap.body, "_quarantine") {
		t.Errorf("shadow DDL should not touch quarantine; got:\n%s", cap.body)
	}
	if err := c.CreateShadowTable(context.Background(), ds, ""); err == nil {
		t.Error("empty shadow name should error")
	}
}

func TestBackfillDDL(t *testing.T) {
	c, cap := newCaptureClient(t)
	if err := c.Backfill(context.Background(), "events_shadow", "events", []string{"user_id", "ts"}); err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	want := "INSERT INTO `events_shadow` (`user_id`, `ts`) SELECT `user_id`, `ts` FROM `events`"
	if cap.body != want {
		t.Errorf("DDL = %q, want %q", cap.body, want)
	}
	cap.body = ""
	if err := c.Backfill(context.Background(), "dst", "src", nil); err == nil {
		t.Error("empty column list should error")
	}
	if cap.body != "" {
		t.Errorf("no request should be sent for an empty backfill, got %q", cap.body)
	}
}

func TestDropTableDDL(t *testing.T) {
	c, cap := newCaptureClient(t)
	if err := c.DropTable(context.Background(), "events_shadow"); err != nil {
		t.Fatalf("DropTable: %v", err)
	}
	if want := "DROP TABLE IF EXISTS `events_shadow`"; cap.body != want {
		t.Errorf("DDL = %q, want %q", cap.body, want)
	}
}

func TestWithDatabaseScopesQueries(t *testing.T) {
	c, cap := newCaptureClient(t) // configured database = tr_main
	if _, err := c.Query(context.Background(), "SELECT 1", nil, nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if cap.db != "tr_main" {
		t.Errorf("original client targeted %q, want tr_main", cap.db)
	}

	scoped := c.WithDatabase("tr_branch")
	if _, err := scoped.Query(context.Background(), "SELECT 1", nil, nil); err != nil {
		t.Fatalf("scoped Query: %v", err)
	}
	if cap.db != "tr_branch" {
		t.Errorf("scoped client targeted %q, want tr_branch", cap.db)
	}
	// Re-scoping must not mutate the original.
	if _, err := c.Query(context.Background(), "SELECT 1", nil, nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if cap.db != "tr_main" {
		t.Errorf("original client mutated to %q, want tr_main", cap.db)
	}
}

func TestIdentQuotesBacktick(t *testing.T) {
	if got := ident("a`b"); got != "`a``b`" {
		t.Errorf("ident(`a`+backtick+`b`) = %q, want escaped backtick", got)
	}
}
