//go:build integration

// Integration tests for the Redis-backed token store. Run with:
//
//	go test -tags=integration ./internal/auth/...
//
// Set TR_TEST_REDIS_ADDR (default localhost:6379). Skips if Redis is
// unreachable so CI without the service container stays green.
package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/tinyraven/tinyraven/internal/model"
)

func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TR_TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis unreachable at %s: %v", addr, err)
	}
	return rdb
}

func TestPutValidateBootstrap(t *testing.T) {
	rdb := testRedis(t)
	defer rdb.Close()
	s := NewStore(rdb)
	ctx := context.Background()

	const val = "tr_integration_test_token"
	t.Cleanup(func() { rdb.Del(ctx, key(val)) })

	// Unknown token -> ok=false, no error.
	if _, ok, err := s.Validate(ctx, val); err != nil || ok {
		t.Fatalf("Validate(unknown) = (ok=%v, err=%v), want (false,nil)", ok, err)
	}

	if err := s.Put(ctx, &model.Token{Name: "t", Value: val, Scopes: []string{"READ:events"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	tok, ok, err := s.Validate(ctx, val)
	if err != nil || !ok {
		t.Fatalf("Validate after Put = (ok=%v, err=%v)", ok, err)
	}
	if !tok.HasScope("READ:events") {
		t.Fatalf("scopes lost: %+v", tok.Scopes)
	}

	// Bootstrap overwrites with ADMIN.
	if err := s.Bootstrap(ctx, val); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	tok, _, _ = s.Validate(ctx, val)
	if !tok.HasScope("ADMIN") || tok.Name != "admin" {
		t.Fatalf("bootstrap token = %+v, want admin/ADMIN", tok)
	}
}
