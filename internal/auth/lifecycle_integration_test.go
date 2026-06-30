//go:build integration

// Additional lifecycle integration tests for the Redis-backed token store,
// covering the readiness probe, Bootstrap idempotency, and the negative
// DeleteByName path. Reuses testRedis from tokens_integration_test.go and the
// same TR_TEST_REDIS_ADDR convention. Run with:
//
//	go test -tags=integration ./internal/auth/...
package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestPing exercises the readiness probe against a live Redis (ADR 0024).
func TestPing(t *testing.T) {
	s := NewStore(testRedis(t))
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping against live redis = %v, want nil", err)
	}
}

// TestBootstrapIdempotent verifies admin-token seeding is safe to repeat: the
// second Bootstrap overwrites the first under the same key, leaving exactly one
// admin/ADMIN credential (no duplicates).
func TestBootstrapIdempotent(t *testing.T) {
	rdb := testRedis(t)
	s := NewStore(rdb)
	ctx := context.Background()

	val := fmt.Sprintf("tr_it_boot_%d_%d", os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { rdb.Del(ctx, key(val)) })

	for i := 1; i <= 2; i++ {
		if err := s.Bootstrap(ctx, val); err != nil {
			t.Fatalf("Bootstrap #%d: %v", i, err)
		}
	}

	tok, ok, err := s.Validate(ctx, val)
	if err != nil || !ok {
		t.Fatalf("Validate after repeated Bootstrap = (ok=%v, err=%v), want (true,nil)", ok, err)
	}
	if tok.Name != "admin" || !tok.HasScope("ADMIN") {
		t.Fatalf("idempotent bootstrap token = %+v, want admin/ADMIN", tok)
	}
	// Repeated seeding must not fan out into multiple keys.
	if got := rdb.Exists(ctx, key(val)).Val(); got != 1 {
		t.Fatalf("Exists(%s) = %d after two Bootstraps, want 1", key(val), got)
	}
}

// TestDeleteByNameUnknown covers the not-found branch: deleting a name that was
// never stored reports (false, nil) rather than an error.
func TestDeleteByNameUnknown(t *testing.T) {
	s := NewStore(testRedis(t))
	ctx := context.Background()

	name := fmt.Sprintf("it_absent_%d_%d", os.Getpid(), time.Now().UnixNano())
	ok, err := s.DeleteByName(ctx, name)
	if err != nil {
		t.Fatalf("DeleteByName(absent) err = %v, want nil", err)
	}
	if ok {
		t.Fatal("DeleteByName(absent) ok = true, want false")
	}
}
