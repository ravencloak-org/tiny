// Package deploy implements `tr deploy`: it parses every .datasource/.pipe file
// in a project directory, validates them all before touching ClickHouse (ADR
// 0027 — validate-all-then-apply), diffs each datasource against the live
// ClickHouse schema, applies safe additive migrations, creates materialized
// views (ADR 0010), and registers the datasource definitions in the metadata
// registry (ADR 0001). Breaking changes (dropped columns, type changes) are
// refused by default; with AllowBreaking they are applied via the shadow-table →
// backfill → EXCHANGE TABLES path (ADR 0007). Options.Database targets a
// per-branch workspace database.
package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinyraven/tinyraven/internal/clickhouse"
	"github.com/tinyraven/tinyraven/internal/datasource"
	"github.com/tinyraven/tinyraven/internal/model"
	"github.com/tinyraven/tinyraven/internal/pipe"
)

// CH is the slice of *clickhouse.Client deploy needs: read-only schema queries,
// table creation, materialized views, and the breaking-migration DDL (shadow
// create / backfill / exchange / drop). Declared as an interface so the
// diff/apply logic is unit testable with a fake; cmd/tr passes the concrete
// *clickhouse.Client.
type CH interface {
	model.CHQuerier // Query(ctx, sql, params, settings) ([]byte, error)
	EnsureTable(ctx context.Context, ds *model.Datasource) error
	CreateDatabase(ctx context.Context, name string) error
	CreateMaterializedView(ctx context.Context, m *model.Materialization) error
	CreateShadowTable(ctx context.Context, ds *model.Datasource, shadowName string) error
	Backfill(ctx context.Context, dst, src string, cols []string) error
	ExchangeTables(ctx context.Context, a, b string) error
	DropTable(ctx context.Context, name string) error
}

// dbScoper is the optional database-scoping capability of CH. The concrete
// *clickhouse.Client satisfies it (WithDatabase returns *clickhouse.Client, which
// is itself a CH). When Options.Database is set, Run creates that database and
// re-scopes the client onto it for all subsequent DDL (ADR 0007). A CH that does
// not implement it (e.g. a test fake) keeps running against its single database
// after CreateDatabase.
//
// ponytail: this is the one spot deploy touches the concrete clickhouse type;
// the rest of the apply logic stays behind the CH interface for fake-based tests.
type dbScoper interface {
	WithDatabase(db string) *clickhouse.Client
}

// Options controls a deploy run.
type Options struct {
	// AllowBreaking acknowledges breaking schema changes. With it set, breaking
	// datasource changes are applied via shadow-table → backfill → EXCHANGE
	// TABLES (ADR 0007); without it they are refused and nothing is applied.
	AllowBreaking bool

	// Database, when non-empty, is the workspace database the deploy targets
	// (the orchestrator passes the per-branch name tr_<branch>; ADR 0007). Run
	// creates it (CREATE DATABASE IF NOT EXISTS) and re-scopes the client onto it
	// before any table DDL. Empty keeps the client's configured database.
	Database string
}

// Report summarizes a deploy. Created lists datasources whose tables were
// created; AltersApplied lists the additive ALTER statements run; Breaking lists
// detected breaking changes; BreakingApplied lists the breaking migrations
// actually performed (only when AllowBreaking); MaterializedViews lists the MVs
// ensured (ADR 0010).
type Report struct {
	Datasources       int
	Pipes             int
	Created           []string
	AltersApplied     []string
	Breaking          []string
	BreakingApplied   []string
	MaterializedViews []string
}

// Run validates and applies the project in dir.
//
// Order (ADR 0027): parse + validate ALL files first; abort before any mutation
// if any file is invalid. If Options.Database is set, create and target that
// workspace database (ADR 0007). Then diff every datasource against the live
// schema. If breaking changes exist and AllowBreaking is false, refuse before
// applying anything (ADR 0006). Otherwise apply creates, breaking shadow swaps,
// and additive alters; register each datasource; then ensure materialized views
// last, once their target tables exist (ADR 0010).
func Run(ctx context.Context, dir string, ch CH, dsReg model.DatasourceRegistry, opts Options) (*Report, error) {
	dss, pipes, err := parseAll(dir)
	if err != nil {
		return nil, err
	}

	report := &Report{Datasources: len(dss), Pipes: len(pipes)}

	// Branch targeting (ADR 0007): create the workspace database and re-scope the
	// client onto it before any diff or DDL. CreateDatabase runs on the client's
	// current (existing) database; the re-scope only takes effect for a CH that
	// exposes WithDatabase (the real client) — all the orchestrator ever passes.
	if opts.Database != "" {
		if err := ch.CreateDatabase(ctx, opts.Database); err != nil {
			return report, err
		}
		if sc, ok := ch.(dbScoper); ok {
			ch = sc.WithDatabase(opts.Database)
		}
	}

	// Diff pass: compute the plan for each datasource without mutating anything,
	// so a refusal leaves ClickHouse untouched.
	type plan struct {
		ds       *model.Datasource
		create   bool
		adds     []string // additive ALTER statements
		breaking bool     // type change and/or dropped column -> needs a shadow swap
		overlap  []string // same-name/same-type columns -> safe to backfill verbatim
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
				pl.breaking = true
				report.Breaking = append(report.Breaking, fmt.Sprintf(
					"%s.%s: type change %s -> %s", ds.Name, col.Name, liveType, col.Type))
			default:
				// Same name and type -> survives a shadow swap and is backfilled.
				pl.overlap = append(pl.overlap, col.Name)
			}
		}
		// Columns live in ClickHouse but dropped from the file -> breaking.
		for liveName := range live {
			if !fileCols[liveName] {
				pl.breaking = true
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

	// Apply pass: create new tables, rebuild breaking ones via shadow swap, apply
	// additive alters to the rest, and register every datasource.
	for _, pl := range plans {
		switch {
		case pl.create:
			if err := ch.EnsureTable(ctx, pl.ds); err != nil {
				return report, err
			}
			report.Created = append(report.Created, pl.ds.Name)
		case pl.breaking:
			// AllowBreaking is guaranteed here (refusal returned above). The shadow
			// carries the full new schema, so pl.adds are subsumed and skipped.
			if err := applyBreaking(ctx, ch, pl.ds, pl.overlap); err != nil {
				return report, err
			}
			report.BreakingApplied = append(report.BreakingApplied, fmt.Sprintf(
				"%s: rebuilt via shadow swap (%d columns backfilled)", pl.ds.Name, len(pl.overlap)))
		default:
			for _, alter := range pl.adds {
				if _, err := ch.Query(ctx, alter, nil, nil); err != nil {
					return report, fmt.Errorf("apply migration %q: %w", alter, err)
				}
				report.AltersApplied = append(report.AltersApplied, alter)
			}
		}
		if err := dsReg.Put(ctx, pl.ds); err != nil {
			return report, fmt.Errorf("register datasource %q: %w", pl.ds.Name, err)
		}
	}

	// Materialized-view pass (ADR 0010): once all tables exist, wire each
	// materialization pipe's MV into its target table. Idempotent (IF NOT EXISTS).
	known := make(map[string]bool, len(dss))
	for _, ds := range dss {
		known[ds.Name] = true
	}
	for _, p := range pipes {
		if p.Material == nil {
			continue
		}
		if err := ensureMaterialization(ctx, ch, p.Material, known); err != nil {
			return report, err
		}
		report.MaterializedViews = append(report.MaterializedViews, p.Material.Name)
	}

	return report, nil
}

// applyBreaking rewrites ds's table to its new schema with no data-visibility gap
// (ADR 0007): create a shadow holding the new schema, backfill the surviving
// columns, atomically EXCHANGE the shadow into the live name, then drop the old
// (now shadow) table. overlap is the set of same-name/same-type columns copied
// verbatim.
//
// ponytail: type-changed and dropped columns are absent from overlap, so they are
// not backfilled and take their new-schema CH defaults. A full rewrite (empty
// overlap) skips the backfill. A genuinely incompatible copy would surface as a
// CHError from Backfill.
func applyBreaking(ctx context.Context, ch CH, ds *model.Datasource, overlap []string) error {
	shadow := ds.Name + "_shadow"
	// Clear any shadow left by an interrupted migration before recreating it.
	if err := ch.DropTable(ctx, shadow); err != nil {
		return err
	}
	if err := ch.CreateShadowTable(ctx, ds, shadow); err != nil {
		return err
	}
	if len(overlap) > 0 {
		if err := ch.Backfill(ctx, shadow, ds.Name, overlap); err != nil {
			return err
		}
	}
	if err := ch.ExchangeTables(ctx, ds.Name, shadow); err != nil {
		return err
	}
	return ch.DropTable(ctx, shadow) // shadow now holds the old table
}

// ensureMaterialization creates the MV for a materialization pipe after verifying
// its target table exists (ADR 0010). The target is normally one of the project's
// datasources (known); otherwise a live-schema check lets an MV target a
// pre-existing table.
func ensureMaterialization(ctx context.Context, ch CH, m *model.Materialization, known map[string]bool) error {
	if m.TargetTable == "" {
		return fmt.Errorf("materialization %q has no target table", m.Name)
	}
	if !known[m.TargetTable] {
		live, err := liveColumns(ctx, ch, m.TargetTable)
		if err != nil {
			return fmt.Errorf("check materialization target %q: %w", m.TargetTable, err)
		}
		if len(live) == 0 {
			return fmt.Errorf("materialization %q target table %q does not exist", m.Name, m.TargetTable)
		}
	}
	return ch.CreateMaterializedView(ctx, m)
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
