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
	"regexp"
	"sort"
	"strings"

	"github.com/tinyraven/tinyraven/internal/auth"
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

// TokenStore is the subset of token operations deploy needs to materialize
// resource tokens (ADR 0030). It is wider than model.TokenStore (which only
// validates + puts, keyed by value) because the idempotent, never-rotate upsert
// must find an existing token by Name — and that requires List. *auth.Store
// satisfies it; cmd/tr passes auth.NewStore(rdb) into Options.Tokens.
type TokenStore interface {
	List(ctx context.Context) ([]*model.Token, error)
	Put(ctx context.Context, t *model.Token) error
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

	// Tokens, when non-nil, makes Run materialize the resource tokens declared via
	// `TOKEN "name" READ|APPEND` lines in .pipe/.datasource files (ADR 0030). Nil
	// skips token materialization entirely, keeping Run backward-compatible with
	// callers that have no token store wired.
	Tokens TokenStore

	// DryRun computes the full plan — validation, schema diff, breaking-change
	// detection, MVs, tokens — and populates the Report, but applies NOTHING: no
	// ClickHouse DDL, no registry writes, no token mint. Backs `tr deploy --check`.
	DryRun bool
}

// Report summarizes a deploy. Created lists datasources whose tables were
// created; AltersApplied lists the additive ALTER statements run; Breaking lists
// detected breaking changes; BreakingApplied lists the breaking migrations
// actually performed (only when AllowBreaking); MaterializedViews lists the MVs
// ensured (ADR 0010); Tokens lists the resource-token names materialized (or,
// on a dry run, that would be materialized) from file declarations (ADR 0030).
type Report struct {
	Datasources       int
	Pipes             int
	Created           []string
	AltersApplied     []string
	Breaking          []string
	BreakingApplied   []string
	MaterializedViews []string
	CopyPipes         []string
	Tokens            []string
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
		// CreateDatabase is a mutation, skipped on a dry run. We still re-scope the
		// client so the diff queries below target the branch database (read-only).
		if !opts.DryRun {
			if err := ch.CreateDatabase(ctx, opts.Database); err != nil {
				return report, err
			}
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
	// additive alters to the rest, and register every datasource. On a dry run the
	// plan is recorded into the Report but every mutation is skipped.
	for _, pl := range plans {
		switch {
		case pl.create:
			if !opts.DryRun {
				if err := ch.EnsureTable(ctx, pl.ds); err != nil {
					return report, err
				}
			}
			report.Created = append(report.Created, pl.ds.Name)
		case pl.breaking:
			// AllowBreaking is guaranteed here (refusal returned above). The shadow
			// carries the full new schema, so pl.adds are subsumed and skipped.
			if !opts.DryRun {
				if err := applyBreaking(ctx, ch, pl.ds, pl.overlap); err != nil {
					return report, err
				}
			}
			report.BreakingApplied = append(report.BreakingApplied, fmt.Sprintf(
				"%s: rebuilt via shadow swap (%d columns backfilled)", pl.ds.Name, len(pl.overlap)))
		default:
			for _, alter := range pl.adds {
				if !opts.DryRun {
					if _, err := ch.Query(ctx, alter, nil, nil); err != nil {
						return report, fmt.Errorf("apply migration %q: %w", alter, err)
					}
				}
				report.AltersApplied = append(report.AltersApplied, alter)
			}
		}
		if !opts.DryRun {
			if err := dsReg.Put(ctx, pl.ds); err != nil {
				return report, fmt.Errorf("register datasource %q: %w", pl.ds.Name, err)
			}
		}
	}

	// Materialized-view pass (ADR 0010): once all tables exist, wire each
	// materialization pipe's MV into its target table. Idempotent (IF NOT EXISTS).
	// Target validation runs even on a dry run; only the CREATE is skipped.
	known := make(map[string]bool, len(dss))
	for _, ds := range dss {
		known[ds.Name] = true
	}
	for _, p := range pipes {
		if p.Material == nil {
			continue
		}
		if err := ensureMaterialization(ctx, ch, p.Material, known, !opts.DryRun); err != nil {
			return report, err
		}
		report.MaterializedViews = append(report.MaterializedViews, p.Material.Name)
	}

	// Copy-pipe pass (gap #9): validate each TYPE copy pipe's target datasource
	// exists. No DDL — the INSERT runs on trigger (POST /v0/pipes/{name}/copy).
	// ponytail: COPY_SCHEDULE is parsed and surfaced but not auto-executed yet
	// (no in-process scheduler / jobs surface); on-demand triggering is the
	// implemented path. Target validation runs even on a dry run.
	for _, p := range pipes {
		if p.Copy == nil {
			continue
		}
		if err := validateCopyTarget(ctx, ch, p.Copy, known); err != nil {
			return report, err
		}
		report.CopyPipes = append(report.CopyPipes, p.Copy.Name)
	}

	// Resource-token materialization (ADR 0030): mint/upsert the tokens declared
	// in the project files. On a dry run we report the planned names but mint
	// nothing; with no store wired we skip entirely (backward-compatible).
	declared := scanTokens(dss, pipes)
	switch {
	case opts.DryRun:
		report.Tokens = sortedKeys(declared)
	case opts.Tokens != nil:
		names, err := materializeTokens(ctx, opts.Tokens, declared)
		if err != nil {
			return report, err
		}
		report.Tokens = names
	}

	return report, nil
}

// scanTokens computes the union of declared scopes per token name across every
// project file (ADR 0030). The structured parsers ignore the TOKEN directive, so
// deploy scans the raw file text: a `TOKEN "x" READ` line in pipe p contributes
// scope READ:p; in datasource d it contributes READ:d (or APPEND:d). The same
// name declared in several files yields one token whose scope is the union. The
// resource name is the file basename, which is exactly the parsed Name.
func scanTokens(dss []*model.Datasource, pipes []*model.Pipe) map[string][]string {
	sets := map[string]map[string]bool{}
	add := func(raw, resource string) {
		for _, m := range tokenDeclRe.FindAllStringSubmatch(raw, -1) {
			name, scope := m[1], strings.ToUpper(m[2])+":"+resource
			if sets[name] == nil {
				sets[name] = map[string]bool{}
			}
			sets[name][scope] = true
		}
	}
	for _, ds := range dss {
		add(ds.Raw, ds.Name)
	}
	for _, p := range pipes {
		add(p.Raw, p.Name)
	}
	out := make(map[string][]string, len(sets))
	for name, set := range sets {
		out[name] = sortedKeys(set)
	}
	return out
}

// tokenDeclRe matches a `TOKEN "name" READ|APPEND` directive line, tolerating
// leading whitespace and case (ADR 0030). Group 1 is the token name; group 2 is
// the permission keyword.
var tokenDeclRe = regexp.MustCompile(`(?mi)^\s*TOKEN\s+"([^"]+)"\s+([A-Za-z]+)`)

// materializeTokens upserts file-declared resource tokens (ADR 0030). For each
// declared name an existing token keeps its value and only its scopes are
// rewritten (never rotate — rotation would break live clients); a brand-new name
// gets a freshly generated value. Orphans (managed before, no longer declared)
// are intentionally left untouched: revocation is an explicit act (`tr token rm`
// / `--prune-tokens`), not a deploy side effect. Returns the materialized names,
// sorted.
func materializeTokens(ctx context.Context, store TokenStore, declared map[string][]string) ([]string, error) {
	existing, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	byName := make(map[string]*model.Token, len(existing))
	for _, t := range existing {
		byName[t.Name] = t
	}
	names := sortedKeys(declared)
	for _, name := range names {
		tok := &model.Token{Name: name, Scopes: declared[name]}
		if cur, ok := byName[name]; ok {
			tok.Value = cur.Value // idempotent: reuse the existing value, never rotate
		} else {
			v, err := auth.GenerateValue()
			if err != nil {
				return nil, fmt.Errorf("generate value for token %q: %w", name, err)
			}
			tok.Value = v
		}
		if err := store.Put(ctx, tok); err != nil {
			return nil, fmt.Errorf("materialize token %q: %w", name, err)
		}
	}
	return names, nil
}

// sortedKeys returns the keys of m sorted lexically. Works for any map keyed by
// string (scope sets, the declared-token map).
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
func ensureMaterialization(ctx context.Context, ch CH, m *model.Materialization, known map[string]bool, apply bool) error {
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
	if !apply {
		return nil // dry run: target validated, MV creation skipped
	}
	return ch.CreateMaterializedView(ctx, m)
}

// validateCopyTarget checks that a copy pipe's target datasource exists before
// the pipe is wired (gap #9): normally one of the project's datasources (known),
// otherwise a live-schema check allows targeting a pre-existing table. No DDL is
// emitted — the copy's INSERT runs at trigger time.
func validateCopyTarget(ctx context.Context, ch CH, c *model.Copy, known map[string]bool) error {
	if c.TargetDatasource == "" {
		return fmt.Errorf("copy pipe %q has no target datasource", c.Name)
	}
	if known[c.TargetDatasource] {
		return nil
	}
	live, err := liveColumns(ctx, ch, c.TargetDatasource)
	if err != nil {
		return fmt.Errorf("check copy target %q: %w", c.TargetDatasource, err)
	}
	if len(live) == 0 {
		return fmt.Errorf("copy pipe %q target datasource %q does not exist", c.Name, c.TargetDatasource)
	}
	return nil
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
		// A not-yet-created workspace DB (common on --check for a fresh branch, or
		// when DryRun skips CreateDatabase) has no live schema → treat as "table
		// absent" so the plan is to create everything, rather than erroring.
		if strings.Contains(err.Error(), "UNKNOWN_DATABASE") || strings.Contains(err.Error(), "doesn't exist") || strings.Contains(err.Error(), "does not exist") {
			return map[string]string{}, nil
		}
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
