package deploy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// fakeCH implements deploy.CH: it serves canned live columns for system.columns
// queries and records every statement it is asked to run.
type fakeCH struct {
	live    map[string][]model.Column // table name -> live columns; absent => table missing
	queries []string                  // every SQL passed to Query, in order
	ensured []string                  // tables passed to EnsureTable
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

func TestRun_BreakingAcknowledgedReturnsPhase3Error(t *testing.T) {
	dir := writeProject(t, map[string]string{
		// user_id dropped (breaking), country added (additive).
		"events.datasource": "SCHEMA >\n    ts DateTime,\n    country String\n",
	})
	ch := &fakeCH{live: map[string][]model.Column{
		"events": {{Name: "user_id", Type: "String"}, {Name: "ts", Type: "DateTime"}},
	}}
	reg := newMemReg()

	report, err := Run(context.Background(), dir, ch, reg, Options{AllowBreaking: true})
	if err == nil || !strings.Contains(err.Error(), "Phase 3") {
		t.Fatalf("err = %v, want Phase 3 explanatory error", err)
	}
	if len(report.Breaking) != 1 || !strings.Contains(report.Breaking[0], "user_id: column dropped") {
		t.Errorf("Breaking = %v, want user_id dropped", report.Breaking)
	}
	// The safe additive change is still applied when breaking is acknowledged.
	if len(report.AltersApplied) != 1 || !strings.Contains(report.AltersApplied[0], "`country`") {
		t.Errorf("AltersApplied = %v, want country ADD COLUMN", report.AltersApplied)
	}
	if _, ok := reg.m["events"]; !ok {
		t.Error("datasource should be registered when breaking is acknowledged")
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
