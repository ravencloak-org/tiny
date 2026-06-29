// Package buildanchor pins subsystem dependencies in go.mod/go.sum before the
// parallel package work starts, so concurrent agents never race on go.mod.
// Deleted at integration once real imports exist.
package buildanchor

import (
	_ "github.com/ClickHouse/clickhouse-go/v2"
	_ "github.com/redis/go-redis/v9"
)
