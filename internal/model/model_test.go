package model

import "testing"

// HasScope gates API access (ADR 0005): a token carries a scope iff the exact
// scope string is present, OR it holds the ADMIN super-scope which grants any.
func TestTokenHasScope(t *testing.T) {
	cases := []struct {
		name   string
		scopes []string
		query  string
		want   bool
	}{
		{"exact match", []string{"READ:hits"}, "READ:hits", true},
		{"admin grants anything", []string{"ADMIN"}, "READ:hits", true},
		{"admin grants admin", []string{"ADMIN"}, "ADMIN", true},
		{"miss", []string{"READ:hits"}, "READ:other", false},
		{"no scopes", nil, "READ:hits", false},
		{"empty query not granted by normal scope", []string{"READ:hits"}, "", false},
		{"scope is case-sensitive", []string{"read:hits"}, "READ:hits", false},
		{"one of several", []string{"READ:a", "READ:b", "WRITE:c"}, "READ:b", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tok := Token{Name: "t", Value: "tr_x", Scopes: c.scopes}
			if got := tok.HasScope(c.query); got != c.want {
				t.Errorf("HasScope(%q) with %v = %v, want %v", c.query, c.scopes, got, c.want)
			}
		})
	}
}

// QuarantineTable derives the per-datasource quarantine table name (ADR 0018).
func TestDatasourceQuarantineTable(t *testing.T) {
	d := Datasource{Name: "events"}
	if got, want := d.QuarantineTable(), "events_quarantine"; got != want {
		t.Errorf("QuarantineTable() = %q, want %q", got, want)
	}
}
