package auth

import (
	"strings"
	"testing"
)

func TestGenerateValue(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		v, err := GenerateValue()
		if err != nil {
			t.Fatalf("GenerateValue: %v", err)
		}
		if !strings.HasPrefix(v, "tr_") {
			t.Fatalf("missing tr_ prefix: %q", v)
		}
		if len(v) < 24 {
			t.Fatalf("suspiciously short token: %q", v)
		}
		if seen[v] {
			t.Fatalf("duplicate token generated: %q", v)
		}
		seen[v] = true
	}
}
