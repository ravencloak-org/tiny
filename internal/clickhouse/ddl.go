package clickhouse

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tinyraven/tinyraven/internal/model"
)

// EnsureTable creates the datasource's table and (for MergeTree datasources) its
// quarantine sibling if they don't exist. This is the MVP bootstrap DDL for local
// dev — full schema diffing and safe migrations are tr deploy's job (Phase 2).
// The target database is assumed to exist (provisioned by the container/operator).
//
// Connector engines (Kafka/S3/PostgreSQL; ADR 0019) get NO quarantine sibling:
// quarantine is the landing zone for rows the Gatherer rejects on the HTTP events
// path (ADR 0018), and connector tables are pulled by ClickHouse, never ingested
// through the Gatherer, so a quarantine table would misrepresent the data path.
func (c *Client) EnsureTable(ctx context.Context, ds *model.Datasource) error {
	if _, err := c.Query(ctx, buildCreateTable(ds.Name, ds), nil, nil); err != nil {
		return fmt.Errorf("create table %s: %w", ds.Name, err)
	}
	if engineFamily(ds.Engine) != familyMergeTree {
		return nil
	}
	if _, err := c.Query(ctx, buildQuarantineTable(ds), nil, nil); err != nil {
		return fmt.Errorf("create quarantine table %s: %w", ds.QuarantineTable(), err)
	}
	return nil
}

// Engine families decide DDL shape. MergeTree-family engines take the existing
// ORDER BY/PARTITION BY/TTL path; connector engines (ADR 0019) render
// engine-specific clauses and carry none of those (they're MergeTree-only).
const (
	familyMergeTree  = "mergetree"
	familyKafka      = "kafka"
	familyS3         = "s3"
	familyPostgreSQL = "postgresql"
)

// engineFamily classifies a CH engine name. It looks only at the head token
// (the part before any "("), case-insensitively, so "ReplacingMergeTree(ver)"
// and "Kafka()" classify correctly. Unknown engines fall through to the
// MergeTree path, preserving prior behaviour.
func engineFamily(engine string) string {
	head := strings.ToLower(strings.TrimSpace(engine))
	if i := strings.IndexByte(head, '('); i >= 0 {
		head = strings.TrimSpace(head[:i])
	}
	switch head {
	case "kafka":
		return familyKafka
	case "s3":
		return familyS3
	case "postgresql", "postgres":
		return familyPostgreSQL
	default:
		return familyMergeTree
	}
}

// buildCreateTable renders the CREATE TABLE DDL for ds under the given table
// name, routing by engine family (ADR 0019). name is separate from ds.Name so
// the breaking-migration flow can stamp the same schema onto a shadow table
// (CreateShadowTable; ADR 0007). Required connector options are enforced at
// parse time (internal/datasource); this builder emits best-effort DDL.
func buildCreateTable(name string, ds *model.Datasource) string {
	engine := ds.Engine
	if engine == "" {
		engine = "MergeTree"
	}
	switch engineFamily(engine) {
	case familyKafka:
		return buildKafkaTable(name, ds)
	case familyS3:
		return buildS3Table(name, ds)
	case familyPostgreSQL:
		return buildPostgreSQLTable(name, ds)
	default:
		return buildMergeTreeTable(name, ds, engine)
	}
}

// columnDefs renders the "  `name` Type" lines shared by every engine's DDL.
func columnDefs(ds *model.Datasource) string {
	cols := make([]string, len(ds.Schema))
	for i, c := range ds.Schema {
		cols[i] = fmt.Sprintf("  %s %s", ident(c.Name), c.Type)
	}
	return strings.Join(cols, ",\n")
}

// buildMergeTreeTable renders MergeTree-family DDL: the original ORDER BY /
// PARTITION BY / TTL path. engine is passed verbatim so parameterised engines
// (e.g. ReplacingMergeTree(ver)) keep their arguments.
//
// ponytail: maps only the three common ENGINE_* options (SORTING_KEY ->
// ORDER BY, PARTITION_KEY -> PARTITION BY, TTL -> TTL); other MergeTree params
// are ignored here. Add them when a datasource needs them.
func buildMergeTreeTable(name string, ds *model.Datasource, engine string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s (\n%s\n) ENGINE = %s\n",
		ident(name), columnDefs(ds), engine)

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

// kafkaSettingOrder fixes the emission order of the well-known Kafka settings so
// the rendered DDL is deterministic and reads naturally; any other ENGINE_KAFKA_*
// options follow, sorted alphabetically.
var kafkaSettingOrder = []string{
	"ENGINE_KAFKA_BROKER_LIST",
	"ENGINE_KAFKA_TOPIC_LIST",
	"ENGINE_KAFKA_GROUP_NAME",
	"ENGINE_KAFKA_FORMAT",
}

// buildKafkaTable renders a ClickHouse Kafka engine table (ADR 0019). Every
// ENGINE_KAFKA_* option becomes a kafka_* SETTING: the key minus the "ENGINE_"
// prefix, lower-cased (ENGINE_KAFKA_BROKER_LIST -> kafka_broker_list). No
// ORDER BY/PARTITION BY/TTL — a Kafka table is a stream, not stored MergeTree
// data; pair it with a materialized view into a MergeTree target.
func buildKafkaTable(name string, ds *model.Datasource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS %s (\n%s\n) ENGINE = Kafka()",
		ident(name), columnDefs(ds))
	if settings := kafkaSettings(ds.EngineOpts); len(settings) > 0 {
		fmt.Fprintf(&b, "\nSETTINGS %s", strings.Join(settings, ", "))
	}
	b.WriteByte('\n')
	return b.String()
}

// kafkaSettings turns ENGINE_KAFKA_* options into "kafka_* = value" clauses,
// well-known keys first (kafkaSettingOrder) then the rest alphabetically.
func kafkaSettings(opts map[string]string) []string {
	var out []string
	seen := map[string]bool{}
	emit := func(key string) {
		v, ok := opts[key]
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		setting := strings.ToLower(strings.TrimPrefix(key, "ENGINE_"))
		out = append(out, fmt.Sprintf("%s = %s", setting, settingValue(v)))
	}
	for _, k := range kafkaSettingOrder {
		emit(k)
	}
	var extra []string
	for k := range opts {
		if strings.HasPrefix(k, "ENGINE_KAFKA_") && !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		emit(k)
	}
	return out
}

// buildS3Table renders a ClickHouse S3 engine table (ADR 0019) from
// ENGINE_S3_* options in CH's positional order: path, [key id, secret,] format,
// [compression]. No ORDER BY/PARTITION BY/TTL.
//
// ponytail: only the static-key auth form is rendered here. IAM roles, session
// tokens, and named collections are deploy-time ClickHouse config, not
// .datasource options — see examples/connectors/README.md.
func buildS3Table(name string, ds *model.Datasource) string {
	o := ds.EngineOpts
	path := firstOpt(o, "ENGINE_S3_PATH", "ENGINE_S3_URL")
	keyID := firstOpt(o, "ENGINE_S3_AWS_ACCESS_KEY_ID", "ENGINE_S3_ACCESS_KEY_ID")
	secret := firstOpt(o, "ENGINE_S3_AWS_SECRET_ACCESS_KEY", "ENGINE_S3_SECRET_ACCESS_KEY")
	format := firstOpt(o, "ENGINE_S3_FORMAT")
	compression := firstOpt(o, "ENGINE_S3_COMPRESSION")

	args := []string{sqlStringLit(path)}
	if keyID != "" || secret != "" {
		args = append(args, sqlStringLit(keyID), sqlStringLit(secret))
	}
	if format != "" {
		args = append(args, sqlStringLit(format))
	}
	if compression != "" {
		args = append(args, sqlStringLit(compression))
	}

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n) ENGINE = S3(%s)\n",
		ident(name), columnDefs(ds), strings.Join(args, ", "))
}

// buildPostgreSQLTable renders a ClickHouse PostgreSQL engine table (ADR 0019):
// PostgreSQL('host:port', 'database', 'table', 'user', 'password'[, 'schema']).
// Accepts both ENGINE_POSTGRES_* and ENGINE_PG_* spellings; port defaults to
// 5432. No ORDER BY/PARTITION BY/TTL.
func buildPostgreSQLTable(name string, ds *model.Datasource) string {
	o := ds.EngineOpts
	host := firstOpt(o, "ENGINE_POSTGRES_HOST", "ENGINE_PG_HOST")
	port := firstOpt(o, "ENGINE_POSTGRES_PORT", "ENGINE_PG_PORT")
	if port == "" {
		port = "5432"
	}
	database := firstOpt(o, "ENGINE_POSTGRES_DATABASE", "ENGINE_PG_DATABASE", "ENGINE_POSTGRES_DB", "ENGINE_PG_DB")
	table := firstOpt(o, "ENGINE_POSTGRES_TABLE", "ENGINE_PG_TABLE")
	user := firstOpt(o, "ENGINE_POSTGRES_USER", "ENGINE_PG_USER")
	password := firstOpt(o, "ENGINE_POSTGRES_PASSWORD", "ENGINE_PG_PASSWORD")
	schema := firstOpt(o, "ENGINE_POSTGRES_SCHEMA", "ENGINE_PG_SCHEMA")

	args := []string{
		sqlStringLit(host + ":" + port),
		sqlStringLit(database),
		sqlStringLit(table),
		sqlStringLit(user),
		sqlStringLit(password),
	}
	if schema != "" {
		args = append(args, sqlStringLit(schema))
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n) ENGINE = PostgreSQL(%s)\n",
		ident(name), columnDefs(ds), strings.Join(args, ", "))
}

// firstOpt returns the first non-empty option among keys (trimmed), or "".
func firstOpt(opts map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(opts[k]); v != "" {
			return v
		}
	}
	return ""
}

// settingValue renders a Kafka SETTING value: bare for numeric literals,
// single-quoted (escaped) otherwise — CH wants string settings quoted and
// numeric ones bare.
func settingValue(v string) string {
	if _, err := strconv.ParseFloat(v, 64); err == nil && v != "" {
		return v
	}
	return sqlStringLit(v)
}

// sqlStringLit wraps s in a single-quoted SQL string literal, doubling embedded
// single quotes. These values come from git-tracked .datasource files, not
// request input, but escaping keeps the DDL well-formed and injection-safe.
func sqlStringLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
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
