package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// fakeCH implements deploy.CH: it serves canned live columns for system.columns
// queries and records every statement it is asked to run. ddl is an ordered log
// of the high-level Phase 3 DDL calls (shadow/backfill/exchange/drop/mv/createdb)
// so tests can assert the migration sequence without a real ClickHouse.
type fakeCH struct {
	live    map[string][]model.Column // table name -> live columns; absent => table missing
	queries []string                  // every SQL passed to Query, in order
	ensured []string                  // tables passed to EnsureTable
	ddl     []string                  // ordered Phase 3 DDL calls
	dbs     []string                  // databases passed to CreateDatabase
	mvs     []string                  // MV names passed to CreateMaterializedView
}

func (f *fakeCH) Query(_ context.Context, sql string, params, _ map[string]string) ([]byte, error) {
	f.queries = append(f.queries, sql)
	if strings.Contains(sql, "system.columns") {
		type col struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		var data []col
		for _, c := range f.live[params["param_tbl"]] {
			data = append(data, col{c.Name, c.Type})
		}
		return json.Marshal(struct {
			Data []col `json:"data"`
		}{data})
	}
	return []byte("{}"), nil
}

func (f *fakeCH) EnsureTable(_ context.Context, ds *model.Datasource) error {
	f.ensured = append(f.ensured, ds.Name)
	return nil
}

func (f *fakeCH) CreateDatabase(_ context.Context, name string) error {
	f.dbs = append(f.dbs, name)
	f.ddl = append(f.ddl, "createdb "+name)
	return nil
}

func (f *fakeCH) CreateMaterializedView(_ context.Context, m *model.Materialization) error {
	f.mvs = append(f.mvs, m.Name)
	f.ddl = append(f.ddl, "mv "+m.Name+"->"+m.TargetTable)
	return nil
}

func (f *fakeCH) CreateShadowTable(_ context.Context, _ *model.Datasource, shadow string) error {
	f.ddl = append(f.ddl, "shadow "+shadow)
	return nil
}

func (f *fakeCH) Backfill(_ context.Context, dst, src string, cols []string) error {
	f.ddl = append(f.ddl, fmt.Sprintf("backfill %s<-%s%v", dst, src, cols))
	return nil
}

func (f *fakeCH) ExchangeTables(_ context.Context, a, b string) error {
	f.ddl = append(f.ddl, "exchange "+a+"/"+b)
	return nil
}

func (f *fakeCH) DropTable(_ context.Context, name string) error {
	f.ddl = append(f.ddl, "drop "+name)
	return nil
}

// memReg is an in-memory model.DatasourceRegistry.
type memReg struct{ m map[string]*model.Datasource }

func newMemReg() *memReg { return &memReg{m: map[string]*model.Datasource{}} }

func (r *memReg) Get(_ context.Context, name string) (*model.Datasource, bool, error) {
	d, ok := r.m[name]
	return d, ok, nil
}
func (r *memReg) Put(_ context.Context, ds *model.Datasource) error { r.m[ds.Name] = ds; return nil }
func (r *memReg) List(_ context.Context) ([]*model.Datasource, error) {
	out := make([]*model.Datasource, 0, len(r.m))
	for _, d := range r.m {
		out = append(out, d)
	}
	return out, nil
}

// writeProject writes the given files (name -> content) into a fresh temp dir.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestRun_CreatesMissingTable(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    user_id String,\n    ts DateTime\n",
		"top.pipe":          "ENDPOINT top\nSQL >\n    SELECT user_id FROM events LIMIT {{Int32(n, 5)}}",
	})
	ch := &fakeCH{live: map[string][]model.Column{}} // no live tables
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Datasources != 1 || report.Pipes != 1 {
		t.Errorf("counts = %d ds / %d pipes, want 1/1", report.Datasources, report.Pipes)
	}
	if len(report.Created) != 1 || report.Created[0] != "events" {
		t.Errorf("Created = %v, want [events]", report.Created)
	}
	if len(ch.ensured) != 1 || ch.ensured[0] != "events" {
		t.Errorf("EnsureTable calls = %v, want [events]", ch.ensured)
	}
	if len(report.AltersApplied) != 0 || len(report.Breaking) != 0 {
		t.Errorf("unexpected alters=%v breaking=%v", report.AltersApplied, report.Breaking)
	}
	if _, ok := reg.m["events"]; !ok {
		t.Error("datasource not registered")
	}
}

func TestRun_AdditiveAlter(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    user_id String,\n    country String\n",
	})
	ch := &fakeCH{live: map[string][]model.Column{
		"events": {{Name: "user_id", Type: "String"}}, // country is new in the file
	}}
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Created) != 0 {
		t.Errorf("Created = %v, want none (table exists)", report.Created)
	}
	if len(report.AltersApplied) != 1 || !strings.Contains(report.AltersApplied[0], "ADD COLUMN IF NOT EXISTS `country` String") {
		t.Fatalf("AltersApplied = %v, want one ADD COLUMN country", report.AltersApplied)
	}
	// The ALTER must have actually been issued to ClickHouse.
	var applied bool
	for _, q := range ch.queries {
		if strings.Contains(q, "ADD COLUMN IF NOT EXISTS `country`") {
			applied = true
		}
	}
	if !applied {
		t.Errorf("ALTER not issued to CH; queries = %v", ch.queries)
	}
}

func TestRun_BreakingRefusedWithoutFlag(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    user_id String,\n    ts Date\n", // ts type changed
	})
	ch := &fakeCH{live: map[string][]model.Column{
		"events": {{Name: "user_id", Type: "String"}, {Name: "ts", Type: "DateTime"}},
	}}
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{AllowBreaking: false})
	if err == nil || !strings.Contains(err.Error(), "--allow-breaking") {
		t.Fatalf("err = %v, want refusal mentioning --allow-breaking", err)
	}
	if len(report.Breaking) != 1 || !strings.Contains(report.Breaking[0], "ts: type change DateTime -> Date") {
		t.Errorf("Breaking = %v, want one ts type change", report.Breaking)
	}
	// Refusal must apply nothing.
	if len(ch.ensured) != 0 || len(report.AltersApplied) != 0 {
		t.Errorf("nothing should be applied on refusal: ensured=%v alters=%v", ch.ensured, report.AltersApplied)
	}
	if len(reg.m) != 0 {
		t.Errorf("no datasource should be registered on refusal, got %v", reg.m)
	}
}

func TestRun_BreakingAppliedViaShadowSwap(t *testing.T) {
	dir := writeProject(t, map[string]string{
		// user_id dropped (breaking), country added (additive), ts unchanged (overlap).
		"events.datasource": "SCHEMA >\n    ts DateTime,\n    country String\n",
	})
	ch := &fakeCH{live: map[string][]model.Column{
		"events": {{Name: "user_id", Type: "String"}, {Name: "ts", Type: "DateTime"}},
	}}
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{AllowBreaking: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Breaking) != 1 || !strings.Contains(report.Breaking[0], "user_id: column dropped") {
		t.Errorf("Breaking = %v, want user_id dropped detected", report.Breaking)
	}
	if len(report.BreakingApplied) != 1 || !strings.Contains(report.BreakingApplied[0], "events") {
		t.Errorf("BreakingApplied = %v, want one events shadow swap", report.BreakingApplied)
	}
	// Additive ADD COLUMN is subsumed by the rebuilt shadow schema, not run.
	if len(report.AltersApplied) != 0 {
		t.Errorf("AltersApplied = %v, want none (subsumed by shadow swap)", report.AltersApplied)
	}
	// The migration must run the exact shadow → backfill → exchange → drop sequence.
	// Only `ts` survives (same name+type); user_id/country are not backfilled.
	want := []string{
		"drop events_shadow",
		"shadow events_shadow",
		"backfill events_shadow<-events[ts]",
		"exchange events/events_shadow",
		"drop events_shadow",
	}
	if strings.Join(ch.ddl, " | ") != strings.Join(want, " | ") {
		t.Errorf("DDL sequence = %v\n want %v", ch.ddl, want)
	}
	if _, ok := reg.m["events"]; !ok {
		t.Error("datasource should be registered after the shadow swap")
	}
}

func TestRun_BreakingFullRewriteSkipsBackfill(t *testing.T) {
	// Every column's type changes -> no overlap -> the backfill step is skipped.
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    n String\n",
	})
	ch := &fakeCH{live: map[string][]model.Column{
		"events": {{Name: "n", Type: "Int64"}},
	}}
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{AllowBreaking: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, c := range ch.ddl {
		if strings.HasPrefix(c, "backfill") {
			t.Errorf("backfill should be skipped on a full rewrite; ddl = %v", ch.ddl)
		}
	}
	if len(report.BreakingApplied) != 1 {
		t.Errorf("BreakingApplied = %v, want one", report.BreakingApplied)
	}
}

func TestRun_MaterializedView(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource":       "SCHEMA >\n    ts DateTime,\n    user_id String\n",
		"daily_rollup.datasource": "SCHEMA >\n    d Date,\n    c UInt64\n",
		"mv_daily.pipe": "MATERIALIZATION mv_daily\nTARGET_TABLE daily_rollup\n" +
			"SQL >\n    SELECT toDate(ts) d, count() c FROM events GROUP BY d",
	})
	ch := &fakeCH{live: map[string][]model.Column{}} // nothing live -> tables created first
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.MaterializedViews) != 1 || report.MaterializedViews[0] != "mv_daily" {
		t.Fatalf("MaterializedViews = %v, want [mv_daily]", report.MaterializedViews)
	}
	if len(ch.mvs) != 1 || ch.mvs[0] != "mv_daily" {
		t.Errorf("CreateMaterializedView calls = %v, want [mv_daily]", ch.mvs)
	}
	// The MV must be created last — after both target and source tables exist.
	last := ch.ddl[len(ch.ddl)-1]
	if !strings.HasPrefix(last, "mv ") {
		t.Errorf("last DDL op = %q, want the MV creation (tables-first ordering)", last)
	}
}

func TestRun_MaterializationMissingTargetFails(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    ts DateTime\n",
		"mv.pipe": "MATERIALIZATION mv\nTARGET_TABLE nope\n" +
			"SQL >\n    SELECT toDate(ts) d FROM events GROUP BY d",
	})
	ch := &fakeCH{live: map[string][]model.Column{}}
	reg := newMemReg()

	_, err := Run(context.Background(), dir, ch, reg, Options{})
	if err == nil || !strings.Contains(err.Error(), "target table") {
		t.Fatalf("err = %v, want missing-target error", err)
	}
	if len(ch.mvs) != 0 {
		t.Errorf("no MV should be created when the target is missing, got %v", ch.mvs)
	}
}

func TestRun_DatabaseTargetCreatesAndScopes(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    ts DateTime\n",
	})
	ch := &fakeCH{live: map[string][]model.Column{}} // empty branch DB -> create
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{Database: "tr_feature-x"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ch.dbs) != 1 || ch.dbs[0] != "tr_feature-x" {
		t.Fatalf("CreateDatabase calls = %v, want [tr_feature-x]", ch.dbs)
	}
	// CreateDatabase must precede any table DDL.
	if len(ch.ddl) == 0 || ch.ddl[0] != "createdb tr_feature-x" {
		t.Errorf("first DDL op = %v, want createdb first", ch.ddl)
	}
	if len(report.Created) != 1 || report.Created[0] != "events" {
		t.Errorf("Created = %v, want [events]", report.Created)
	}
}

func TestRun_NoDatabaseSkipsCreateDatabase(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    ts DateTime\n",
	})
	ch := &fakeCH{live: map[string][]model.Column{}}
	reg := newMemReg()

	if _, err := Run(context.Background(), dir, ch, reg, Options{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(ch.dbs) != 0 {
		t.Errorf("CreateDatabase should not be called when Options.Database is empty, got %v", ch.dbs)
	}
}

func TestRun_ValidationAggregatesAndAppliesNothing(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"good.datasource": "SCHEMA >\n    a String\n",
		"bad.datasource":  `ENGINE "MergeTree"`, // missing SCHEMA -> validation error
	})
	ch := &fakeCH{live: map[string][]model.Column{}}
	reg := newMemReg()

	_, err := Run(context.Background(), dir, ch, reg, Options{})
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("err = %v, want aggregated validation failure", err)
	}
	if len(ch.ensured) != 0 || len(reg.m) != 0 {
		t.Errorf("no mutation on validation failure: ensured=%v reg=%v", ch.ensured, reg.m)
	}
}

func TestRun_CleanRediffNoChanges(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"events.datasource": "SCHEMA >\n    user_id String,\n    ts DateTime\n",
	})
	ch := &fakeCH{live: map[string][]model.Column{
		"events": {{Name: "user_id", Type: "String"}, {Name: "ts", Type: "DateTime"}},
	}}
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(report.Created) != 0 || len(report.AltersApplied) != 0 || len(report.Breaking) != 0 {
		t.Errorf("expected no changes, got %+v", report)
	}
	if _, ok := reg.m["events"]; !ok {
		t.Error("datasource should still be registered on a clean deploy")
	}
}
