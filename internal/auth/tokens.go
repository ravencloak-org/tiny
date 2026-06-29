// Package auth is the Redis-backed token store (ADR 0005). Tokens are indexed
// by their secret bearer value so Validate is an O(1) GET on the auth hot path.
// It implements model.TokenStore.
package auth

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"

	"github.com/tinyraven/tinyraven/internal/model"
)

// keyPrefix namespaces token keys: tr:token:<value> -> token JSON.
const keyPrefix = "tr:token:"

// Store validates and persists bearer tokens in Redis.
type Store struct {
	rdb *redis.Client
}

// NewStore wraps a redis client.
func NewStore(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

// Validate looks a bearer value up by its O(1) key. ok=false (nil error) when
// the token is unknown — the caller maps that to 403.
func (s *Store) Validate(ctx context.Context, value string) (*model.Token, bool, error) {
	raw, err := s.rdb.Get(ctx, key(value)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	t, err := decodeToken(raw)
	if err != nil {
		return nil, false, err
	}
	return t, true, nil
}

// Put writes (or overwrites) a token, indexed by its value. No TTL — tokens live
// until revoked.
func (s *Store) Put(ctx context.Context, t *model.Token) error {
	b, err := encodeToken(t)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, key(t.Value), b, 0).Err()
}

// Bootstrap creates/overwrites the ADMIN token for value, used at startup from
// config TR_ADMIN_TOKEN to guarantee a working credential exists (ADR 0005).
func (s *Store) Bootstrap(ctx context.Context, value string) error {
	return s.Put(ctx, &model.Token{
		Name:   "admin",
		Value:  value,
		Scopes: []string{"ADMIN"},
	})
}

// Ping satisfies model.Pinger for the readiness probe (ADR 0024).
func (s *Store) Ping(ctx context.Context) error { return s.rdb.Ping(ctx).Err() }

func key(value string) string { return keyPrefix + value }

func encodeToken(t *model.Token) ([]byte, error) { return json.Marshal(t) }

func decodeToken(b []byte) (*model.Token, error) {
	var t model.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
