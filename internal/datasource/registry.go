package datasource

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/tinyraven/tinyraven/internal/model"
)

// keyPrefix namespaces datasource metadata in Redis (ADR 0001). Git
// .datasource files are the source of truth; this registry is the rebuildable
// hot copy populated by tr deploy.
const keyPrefix = "tr:ds:"

// Registry is a Redis-backed model.DatasourceRegistry. Each datasource is
// stored as JSON under "tr:ds:<name>".
type Registry struct {
	rdb *redis.Client
}

// NewRegistry returns a Registry backed by rdb.
func NewRegistry(rdb *redis.Client) *Registry {
	return &Registry{rdb: rdb}
}

var _ model.DatasourceRegistry = (*Registry)(nil)

// Get returns the datasource named name. ok is false (with nil error) when no
// such key exists.
func (r *Registry) Get(ctx context.Context, name string) (*model.Datasource, bool, error) {
	raw, err := r.rdb.Get(ctx, keyPrefix+name).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("datasource get %q: %w", name, err)
	}
	var ds model.Datasource
	if err := json.Unmarshal(raw, &ds); err != nil {
		return nil, false, fmt.Errorf("datasource decode %q: %w", name, err)
	}
	return &ds, true, nil
}

// Put stores ds, overwriting any existing entry. No TTL: the registry is an
// AOF-persisted system of record (ADR 0001).
func (r *Registry) Put(ctx context.Context, ds *model.Datasource) error {
	b, err := json.Marshal(ds)
	if err != nil {
		return fmt.Errorf("datasource encode %q: %w", ds.Name, err)
	}
	if err := r.rdb.Set(ctx, keyPrefix+ds.Name, b, 0).Err(); err != nil {
		return fmt.Errorf("datasource put %q: %w", ds.Name, err)
	}
	return nil
}

// List returns every stored datasource. Keys are discovered via SCAN (never
// KEYS — KEYS blocks the Redis event loop on large keyspaces).
func (r *Registry) List(ctx context.Context) ([]*model.Datasource, error) {
	var keys []string
	iter := r.rdb.Scan(ctx, 0, keyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("datasource scan: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}

	vals, err := r.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("datasource mget: %w", err)
	}
	out := make([]*model.Datasource, 0, len(vals))
	for i, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue // key vanished between SCAN and MGET; skip
		}
		var ds model.Datasource
		if err := json.Unmarshal([]byte(s), &ds); err != nil {
			return nil, fmt.Errorf("datasource decode %q: %w", keys[i], err)
		}
		out = append(out, &ds)
	}
	return out, nil
}
