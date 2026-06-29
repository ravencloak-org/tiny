// Package deploy implements `tr deploy`: it parses every .datasource/.pipe file
// in a project directory, validates them all before touching ClickHouse (ADR
// 0027 — validate-all-then-apply), diffs each datasource against the live
// ClickHouse schema, applies only safe additive migrations, and registers the
// datasource definitions in the metadata registry (ADR 0001). Breaking changes
// (dropped columns, type changes) are detected and reported but never applied —
// the shadow-table → MV backfill → EXCHANGE TABLES path is Phase 3 (ADR 0007).
package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinyraven/tinyraven/internal/datasource"
	"github.com/tinyraven/tinyraven/internal/model"
	"github.com/tinyraven/tinyraven/internal/pipe"
)

// CH is the slice of *clickhouse.Client deploy needs: read-only schema queries
// plus table creation. Declared as an interface so the diff/apply logic is unit
// testable with a fake; cmd/tr passes the concrete *clickhouse.Client.
type CH interface {
	model.CHQuerier // Query(ctx, sql, params, settings) ([]byte, error)
	EnsureTable(ctx context.Context, ds *model.Datasource) error
}

// Options controls a deploy run.
type Options struct {
	// AllowBreaking acknowledges breaking schema changes. They are still not
	// applied in Phase 2 (ADR 0007); the run reports them and returns an
	// explanatory error instead of refusing outright.
	AllowBreaking bool
}

// Report summarizes a deploy. Created lists datasources whose tables were
// created; AltersApplied lists the additive ALTER statements run; Breaking
// lists detected breaking changes (never applied).
type Report struct {
	Datasources   int
	Pipes         int
	Created       []string
	AltersApplied []string
	Breaking      []string
}

// Run validates and applies the project in dir.
//
// Order (ADR 0027): parse + validate ALL files first; abort before any mutation
// if any file is invalid. Then diff every datasource against the live schema. If
// breaking changes exist and AllowBreaking is false, refuse before applying
// anything (ADR 0006). Otherwise apply creates + additive alters, register each
// datasource, and — if breaking changes were acknowledged — return the Phase-3
// explanatory error.
func Run(ctx context.Context, dir string, ch CH, dsReg model.DatasourceRegistry, opts Options) (*Report, error) {
	dss, pipes, err := parseAll(dir)
	if err != nil {
		return nil, err
	}

	report := &Report{Datasources: len(dss), Pipes: len(pipes)}

	// Diff pass: compute the plan for each datasource without mutating anything,
	// so a refusal leaves ClickHouse untouched.
	type plan struct {
		ds     *model.Datasource
		create bool
		adds   []string // additive ALTER statements
	}
	var plans []plan
	for _, ds := range dss {
		live, err := liveColumns(ctx, ch, ds.Name)
		if err != nil {
			return nil, fmt.Errorf("read live schema for %q: %w", ds.Name, err)
		}
		if len(live) == 0 {
			plans = append(plans, plan{ds: ds, create: true})
			continue
		}
		pl := plan{ds: ds}
		fileCols := make(map[string]bool, len(ds.Schema))
		for _, col := range ds.Schema {
			fileCols[col.Name] = true
			liveType, ok := live[col.Name]
			switch {
			case !ok:
				// New column in file, absent in table -> additive migration.
				pl.adds = append(pl.adds, fmt.Sprintf(
					"ALTER TABLE `%s` ADD COLUMN IF NOT EXISTS `%s` %s", ds.Name, col.Name, col.Type))
			case !typesEqual(liveType, col.Type):
				report.Breaking = append(report.Breaking, fmt.Sprintf(
					"%s.%s: type change %s -> %s", ds.Name, col.Name, liveType, col.Type))
			}
		}
		// Columns live in ClickHouse but dropped from the file -> breaking.
		for liveName := range live {
			if !fileCols[liveName] {
				report.Breaking = append(report.Breaking, fmt.Sprintf(
					"%s.%s: column dropped", ds.Name, liveName))
			}
		}
		plans = append(plans, pl)
	}
	sort.Strings(report.Breaking)

	// Refuse breaking changes unless acknowledged — nothing has been applied yet
	// (ADR 0006).
	if len(report.Breaking) > 0 && !opts.AllowBreaking {
		return report, fmt.Errorf(
			"breaking schema changes detected (refused; ADR 0006), nothing applied: %s — re-run with --allow-breaking",
			strings.Join(report.Breaking, "; "))
	}

	// Apply pass: safe creates + additive alters only; register every datasource.
	for _, pl := range plans {
		if pl.create {
			if err := ch.EnsureTable(ctx, pl.ds); err != nil {
				return report, err
			}
			report.Created = append(report.Created, pl.ds.Name)
		}
		for _, alter := range pl.adds {
			if _, err := ch.Query(ctx, alter, nil, nil); err != nil {
				return report, fmt.Errorf("apply migration %q: %w", alter, err)
			}
			report.AltersApplied = append(report.AltersApplied, alter)
		}
		if err := dsReg.Put(ctx, pl.ds); err != nil {
			return report, fmt.Errorf("register datasource %q: %w", pl.ds.Name, err)
		}
	}

	// Breaking changes were acknowledged (AllowBreaking) but cannot be applied
	// yet: shadow table -> MV backfill -> EXCHANGE TABLES is Phase 3 (ADR 0007).
	if len(report.Breaking) > 0 {
		return report, fmt.Errorf(
			"breaking schema changes acknowledged but not applied — shadow-table → MV backfill → EXCHANGE TABLES is Phase 3 (ADR 0007): %s",
			strings.Join(report.Breaking, "; "))
	}

	return report, nil
}

// parseAll parses every .datasource/.pipe under dir, aggregating every parse/
// validation error so the user fixes them in one pass (ADR 0027). On any error
// it returns no parsed files, so the caller applies nothing.
func parseAll(dir string) ([]*model.Datasource, []*model.Pipe, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read project dir %q: %w", dir, err)
	}

	var (
		dss   []*model.Datasource
		pipes []*model.Pipe
		errs  []string
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		switch {
		case strings.HasSuffix(e.Name(), ".datasource"):
			ds, err := datasource.ParseFile(path)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			dss = append(dss, ds)
		case strings.HasSuffix(e.Name(), ".pipe"):
			p, err := pipe.ParseFile(path)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			pipes = append(pipes, p)
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, nil, fmt.Errorf("validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return dss, pipes, nil
}

// liveColumns returns the current ClickHouse columns of table (name -> type) in
// the deploy's target database. An empty map means the table does not exist. The
// table name binds as a CH parameter, never interpolated (ADR 0003).
func liveColumns(ctx context.Context, ch CH, table string) (map[string]string, error) {
	const q = "SELECT name, type FROM system.columns " +
		"WHERE database = currentDatabase() AND table = {tbl:String} FORMAT JSON"
	body, err := ch.Query(ctx, q, map[string]string{"param_tbl": table}, nil)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Data []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode system.columns response: %w", err)
	}
	cols := make(map[string]string, len(parsed.Data))
	for _, c := range parsed.Data {
		cols[c.Name] = c.Type
	}
	return cols, nil
}

// typesEqual compares a file-declared type to a live ClickHouse type. ClickHouse
// reports types canonically, so an exact trimmed string match is sufficient for
// the additive-vs-breaking decision in Phase 2.
// ponytail: no type normalization (Nullable()/aliases). If false positives bite,
// canonicalize here rather than reimplementing ClickHouse's type system.
func typesEqual(live, file string) bool {
	return strings.TrimSpace(live) == strings.TrimSpace(file)
}
