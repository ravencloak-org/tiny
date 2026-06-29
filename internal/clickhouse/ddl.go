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
	if _, err := c.Query(ctx, buildCreateTable(ds), nil, nil); err != nil {
		return fmt.Errorf("create table %s: %w", ds.Name, err)
	}
	if _, err := c.Query(ctx, buildQuarantineTable(ds), nil, nil); err != nil {
		return fmt.Errorf("create quarantine table %s: %w", ds.QuarantineTable(), err)
	}
	return nil
}

func buildCreateTable(ds *model.Datasource) string {
	cols := make([]string, len(ds.Schema))
	for i, c := range ds.Schema {
		cols[i] = fmt.Sprintf("  `%s` %s", c.Name, c.Type)
	}

	engine := ds.Engine
	if engine == "" {
		engine = "MergeTree"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE IF NOT EXISTS `%s` (\n%s\n) ENGINE = %s\n",
		ds.Name, strings.Join(cols, ",\n"), engine)

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
