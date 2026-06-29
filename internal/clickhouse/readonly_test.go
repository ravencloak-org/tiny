package clickhouse

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// captureServer records the auth user header and body of every request it
// receives and always replies 200, so tests can assert which identity a code
// path authenticated as.
type captured struct {
	user string
	key  string
	body string
}

func captureServer(t *testing.T) (*httptest.Server, *[]captured) {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []captured
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, captured{
			user: r.Header.Get("X-ClickHouse-User"),
			key:  r.Header.Get("X-ClickHouse-Key"),
			body: string(b),
		})
		mu.Unlock()
		_, _ = w.Write([]byte("1"))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

// The read path (Query) authenticates as the read-only identity when one is set.
func TestQueryUsesReadonlyIdentityWhenConfigured(t *testing.T) {
	srv, reqs := captureServer(t)
	c, err := New(Config{
		HTTPURL: srv.URL, Database: "tr_main",
		User: "rw_user", Password: "rw_pw",
		ROUser: "ro_user", ROPassword: "ro_pw",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Query(context.Background(), "SELECT 1", nil, nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(*reqs) != 1 {
		t.Fatalf("got %d requests, want 1", len(*reqs))
	}
	if got := (*reqs)[0]; got.user != "ro_user" || got.key != "ro_pw" {
		t.Errorf("read auth = (%q,%q), want (ro_user,ro_pw)", got.user, got.key)
	}
}

// With no read-only identity configured, the read path falls back to the
// read-write user (backward-compatible).
func TestQueryFallsBackToReadWriteWhenNoReadonlyIdentity(t *testing.T) {
	srv, reqs := captureServer(t)
	c, err := New(Config{HTTPURL: srv.URL, Database: "tr_main", User: "rw_user", Password: "rw_pw"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Query(context.Background(), "SELECT 1", nil, nil); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got := (*reqs)[0]; got.user != "rw_user" || got.key != "rw_pw" {
		t.Errorf("read auth = (%q,%q), want (rw_user,rw_pw)", got.user, got.key)
	}
}

// DDL/write helpers must keep using the read-write user even when a read-only
// identity is configured — otherwise readonly=2 would reject them.
func TestDDLUsesReadWriteIdentityEvenWithReadonlyConfigured(t *testing.T) {
	srv, reqs := captureServer(t)
	c, err := New(Config{
		HTTPURL: srv.URL, Database: "tr_main",
		User: "rw_user", Password: "rw_pw",
		ROUser: "ro_user", ROPassword: "ro_pw",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.CreateDatabase(context.Background(), "tr_branch"); err != nil {
		t.Fatalf("CreateDatabase: %v", err)
	}
	if got := (*reqs)[0]; got.user != "rw_user" || got.key != "rw_pw" {
		t.Errorf("DDL auth = (%q,%q), want (rw_user,rw_pw)", got.user, got.key)
	}
}

func TestReadonlyUserStmts(t *testing.T) {
	// Name needs backtick quoting; password with a single quote must be escaped.
	got := readonlyUserStmts("tr-ro", "p'w")
	want := []string{
		"CREATE USER OR REPLACE `tr-ro` IDENTIFIED BY 'p''w' SETTINGS readonly = 2",
		"GRANT SELECT ON *.* TO `tr-ro`",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stmt[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEnsureReadonlyUserRunsStatementsAsReadWrite(t *testing.T) {
	srv, reqs := captureServer(t)
	c, err := New(Config{
		HTTPURL: srv.URL, Database: "tr_main",
		User: "admin", Password: "admin_pw",
		ROUser: "ro_user", ROPassword: "ro_pw",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.EnsureReadonlyUser(context.Background(), "tr_ro", "secret"); err != nil {
		t.Fatalf("EnsureReadonlyUser: %v", err)
	}

	want := readonlyUserStmts("tr_ro", "secret")
	if len(*reqs) != len(want) {
		t.Fatalf("got %d requests, want %d", len(*reqs), len(want))
	}
	for i, r := range *reqs {
		if r.user != "admin" || r.key != "admin_pw" {
			t.Errorf("req[%d] auth = (%q,%q), want admin identity", i, r.user, r.key)
		}
		if r.body != want[i] {
			t.Errorf("req[%d] body = %q, want %q", i, r.body, want[i])
		}
	}
}

func TestEnsureReadonlyUserRejectsEmptyName(t *testing.T) {
	c, _ := New(Config{HTTPURL: "http://localhost:8123", Database: "tr_main", User: "admin"})
	if err := c.EnsureReadonlyUser(context.Background(), "", "pw"); err == nil {
		t.Fatal("expected error on empty name")
	}
}
