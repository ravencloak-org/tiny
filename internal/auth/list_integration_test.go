//go:build integration

package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/tinyraven/tinyraven/internal/model"
)

// TestListDelete exercises List / Delete / DeleteByName against a real Redis.
// Skips if Redis is unreachable.
func TestListDelete(t *testing.T) {
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
	t.Cleanup(func() { rdb.Close() })
	s := NewStore(rdb)

	// Unique names so the test is hermetic on a shared Redis.
	n1 := fmt.Sprintf("it_list_%d_a", time.Now().UnixNano())
	n2 := fmt.Sprintf("it_list_%d_b", time.Now().UnixNano())
	v1, _ := GenerateValue()
	v2, _ := GenerateValue()
	t.Cleanup(func() { _ = s.Delete(context.Background(), v1); _ = s.Delete(context.Background(), v2) })

	for _, tok := range []*model.Token{
		{Name: n1, Value: v1, Scopes: []string{"READ:p"}},
		{Name: n2, Value: v2, Scopes: []string{"ADMIN"}},
	} {
		if err := s.Put(ctx, tok); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	got := map[string]bool{}
	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, tk := range all {
		got[tk.Name] = true
	}
	if !got[n1] || !got[n2] {
		t.Fatalf("List missing our tokens: have n1=%v n2=%v", got[n1], got[n2])
	}

	// DeleteByName removes n1; Validate then misses it.
	ok, err := s.DeleteByName(ctx, n1)
	if err != nil || !ok {
		t.Fatalf("DeleteByName(%s) = (%v,%v)", n1, ok, err)
	}
	if _, found, _ := s.Validate(ctx, v1); found {
		t.Fatal("v1 still validates after DeleteByName")
	}
	// Delete removes n2 by value.
	if err := s.Delete(ctx, v2); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := s.Validate(ctx, v2); found {
		t.Fatal("v2 still validates after Delete")
	}
}
