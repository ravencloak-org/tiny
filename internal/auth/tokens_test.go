package auth

import (
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

func TestKey(t *testing.T) {
	if got := key("abc"); got != "tr:token:abc" {
		t.Fatalf("key = %q, want tr:token:abc", got)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := &model.Token{Name: "admin", Value: "s3cret", Scopes: []string{"ADMIN", "READ:events"}}
	b, err := encodeToken(in)
	if err != nil {
		t.Fatalf("encodeToken: %v", err)
	}
	out, err := decodeToken(b)
	if err != nil {
		t.Fatalf("decodeToken: %v", err)
	}
	if out.Name != in.Name || out.Value != in.Value || len(out.Scopes) != len(in.Scopes) {
		t.Fatalf("round-trip mismatch: %+v != %+v", out, in)
	}
	if !out.HasScope("READ:events") || !out.HasScope("anything") /* ADMIN grants all */ {
		t.Fatalf("scope check failed after round-trip: %+v", out.Scopes)
	}
}

func TestDecodeTokenBadJSON(t *testing.T) {
	if _, err := decodeToken([]byte("{not json")); err == nil {
		t.Fatal("expected decode error on malformed JSON")
	}
}
