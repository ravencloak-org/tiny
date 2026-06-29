package clickhouse

import (
	"context"
	"fmt"
	"strings"

	"github.com/tinyraven/tinyraven/internal/model"
)

// EnsureTable creates the datasource's table and its quarantine sibling if they
// don't exist. This is the MVP bootstrap DDL for local dev — full schema diffing
// and safe migrations are tr deploy's job (Phase 2). The target database is
// assumed to exist (provisioned by the container / operator).
//
// ponytail: maps only the three common ENGINE_* options (SORTING_KEY ->
// ORDER BY, PARTITION_KEY -> PARTITION BY, TTL -> TTL); other engine params are
// ignored here. Add them when a datasource needs them.
func (c *Client) EnsureTable(ctx context.Context, ds *model.Datasource) error {
	if _, err := c.Query(ctx, buildCreateTable(ds.Name, ds), nil, nil); err != nil {
		return fmt.Errorf("create table %s: %w", ds.Name, err)
	}
	if _, err := c.Query(ctx, buildQuarantineTable(ds), nil, nil); err != nil {
		return fmt.Errorf("create quarantine table %s: %w", ds.QuarantineTable(), err)
	}
	return nil
}

// buildCreateTable renders the MergeTree DDL for ds under the given table name.
// name is separate from ds.Name so the breaking-migration flow can stamp the
// same schema onto a shadow table (CreateShadowTable; ADR 0007).
func buildCreateTable(name string, ds *model.Datasource) string {
	cols := make([]string, len(ds.Schema))
	for i, c := range ds.Schema {
		cols[i] = fmt.Sprintf("  %s %s", ident(c.Name), c.Type)
	}

	engine := ds.Engine
	if engine == "" {
		engine = "MergeTree"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s (\n%s\n) ENGINE = %s\n",
		ident(name), strings.Join(cols, ",\n"), engine)

	order := ds.EngineOpts["ENGINE_SORTING_KEY"]
	if order == "" {
		order = "tuple()" // MergeTree requires ORDER BY
	}
	fmt.Fprintf(&b, "ORDER BY %s\n", order)
	if pk := ds.EngineOpts["ENGINE_PARTITION_KEY"]; pk != "" {
		fmt.Fprintf(&b, "PARTITION BY %s\n", pk)
	}
	if ttl := ds.EngineOpts["ENGINE_TTL"]; ttl != "" {
		fmt.Fprintf(&b, "TTL %s\n", ttl)
	}
	return b.String()
}

func buildQuarantineTable(ds *model.Datasource) string {
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS `%s` (\n"+
			"  `raw` String,\n  `error` String,\n  `timestamp` DateTime DEFAULT now()\n"+
			") ENGINE = MergeTree\nORDER BY tuple()",
		ds.QuarantineTable())
}

// ---- Phase 3 DDL: branch databases, materialized views, breaking migrations ----

// CreateDatabase issues CREATE DATABASE IF NOT EXISTS. It runs against the
// client's currently-scoped database, so the orchestrator calls it on a client
// pointed at an existing DB, then WithDatabase to target the new one (ADR 0007).
//
// ponytail: name is backticked but not otherwise sanitized — branch names with
// '-' or '/' (e.g. tr_feature-x) require the quoting. Mapping a git branch to a
// legal CH database name is the orchestrator's job.
func (c *Client) CreateDatabase(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("clickhouse: CreateDatabase requires a name")
	}
	sql := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", ident(name))
	if _, err := c.Query(ctx, sql, nil, nil); err != nil {
		return fmt.Errorf("create database %s: %w", name, err)
	}
	return nil
}

// CreateMaterializedView creates an incremental MV that writes into an existing
// target table (CREATE MATERIALIZED VIEW ... TO <target> AS <select>). The
// target table must already exist; deploy ensures it first (ADR 0010). Idempotent
// via IF NOT EXISTS — re-running deploy never errors on an existing MV.
//
// ponytail: backfill of pre-existing source rows (ADR 0010) is deploy's concern,
// not this builder's; this only wires the forward MV.
func (c *Client) CreateMaterializedView(ctx context.Context, m *model.Materialization) error {
	if m == nil || m.Name == "" {
		return fmt.Errorf("clickhouse: CreateMaterializedView requires a name")
	}
	if m.TargetTable == "" {
		return fmt.Errorf("clickhouse: materialized view %q has no target table", m.Name)
	}
	if strings.TrimSpace(m.SQL) == "" {
		return fmt.Errorf("clickhouse: materialized view %q has empty SQL", m.Name)
	}
	sql := fmt.Sprintf("CREATE MATERIALIZED VIEW IF NOT EXISTS %s TO %s AS %s",
		ident(m.Name), ident(m.TargetTable), m.SQL)
	if _, err := c.Query(ctx, sql, nil, nil); err != nil {
		return fmt.Errorf("create materialized view %s: %w", m.Name, err)
	}
	return nil
}

// ExchangeTables atomically swaps two tables (EXCHANGE TABLES a AND b). It is the
// final, atomic step of a breaking migration: swap the rebuilt shadow into the
// live name with no window of missing data (ADR 0007).
func (c *Client) ExchangeTables(ctx context.Context, a, b string) error {
	if a == "" || b == "" {
		return fmt.Errorf("clickhouse: ExchangeTables requires two table names")
	}
	sql := fmt.Sprintf("EXCHANGE TABLES %s AND %s", ident(a), ident(b))
	if _, err := c.Query(ctx, sql, nil, nil); err != nil {
		return fmt.Errorf("exchange tables %s <-> %s: %w", a, b, err)
	}
	return nil
}

// CreateShadowTable creates shadowName with ds's NEW schema/engine — the first
// step of a breaking migration. Only the main table is created (not the
// quarantine sibling): the swap rewrites event data, while quarantine schema is
// fixed and untouched (ADR 0007).
func (c *Client) CreateShadowTable(ctx context.Context, ds *model.Datasource, shadowName string) error {
	if shadowName == "" {
		return fmt.Errorf("clickhouse: CreateShadowTable requires a shadow name")
	}
	if _, err := c.Query(ctx, buildCreateTable(shadowName, ds), nil, nil); err != nil {
		return fmt.Errorf("create shadow table %s: %w", shadowName, err)
	}
	return nil
}

// Backfill copies cols from src into dst (INSERT INTO dst (cols) SELECT cols FROM
// src) — the second step of a breaking migration, populating the shadow table
// from the live one over the columns that survive the change.
//
// ponytail: cols must be the type-compatible overlap; the caller (deploy) drops
// type-changed and removed columns, which then take their CH defaults in the new
// schema. A genuinely incompatible backfill would error here at the CH layer.
func (c *Client) Backfill(ctx context.Context, dst, src string, cols []string) error {
	if dst == "" || src == "" {
		return fmt.Errorf("clickhouse: Backfill requires src and dst")
	}
	if len(cols) == 0 {
		return fmt.Errorf("clickhouse: Backfill of %s from %s needs at least one column", dst, src)
	}
	list := make([]string, len(cols))
	for i, c := range cols {
		list[i] = ident(c)
	}
	cs := strings.Join(list, ", ")
	sql := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s", ident(dst), cs, cs, ident(src))
	if _, err := c.Query(ctx, sql, nil, nil); err != nil {
		return fmt.Errorf("backfill %s from %s: %w", dst, src, err)
	}
	return nil
}

// DropTable issues DROP TABLE IF EXISTS — the final cleanup after EXCHANGE TABLES
// drops the now-shadow (old) table.
func (c *Client) DropTable(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("clickhouse: DropTable requires a name")
	}
	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", ident(name))
	if _, err := c.Query(ctx, sql, nil, nil); err != nil {
		return fmt.Errorf("drop table %s: %w", name, err)
	}
	return nil
}

// ident backtick-quotes a ClickHouse identifier (database/table/column),
// doubling any embedded backtick. These names come from git-tracked project
// files and branch names, not request input, but quoting keeps DDL well-formed
// for names with '-', reserved words, etc.
func ident(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
