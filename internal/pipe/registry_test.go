package pipe

import (
	"testing"

	"github.com/tinyraven/tinyraven/internal/model"
)

// A fresh registry is empty: List returns nothing and Get misses.
func TestRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Errorf("new registry List() = %v, want empty", got)
	}
	if p, ok := r.Get("nope"); ok || p != nil {
		t.Errorf("Get on empty = (%v, %v), want (nil, false)", p, ok)
	}
}

// Put inserts/replaces a single pipe and publishes a new snapshot; List is
// name-sorted (ADR 0020).
func TestRegistry_PutGetList(t *testing.T) {
	r := NewRegistry()
	r.Put(&model.Pipe{Name: "zeta"})
	r.Put(&model.Pipe{Name: "alpha"})

	if p, ok := r.Get("alpha"); !ok || p == nil || p.Name != "alpha" {
		t.Fatalf("Get(alpha) = (%v, %v), want the alpha pipe", p, ok)
	}

	got := r.List()
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("List() not name-sorted: %v", names(got))
	}

	// Put with an existing name replaces, not duplicates.
	r.Put(&model.Pipe{Name: "alpha", Raw: "v2"})
	if p, _ := r.Get("alpha"); p.Raw != "v2" {
		t.Errorf("Put did not replace existing pipe: Raw=%q", p.Raw)
	}
	if len(r.List()) != 2 {
		t.Errorf("Put on existing name must not grow the set: %v", names(r.List()))
	}
}

// Replace atomically swaps the whole set and defensively copies the input map,
// so later caller mutation cannot leak into the registry (hot-reload path).
func TestRegistry_ReplaceCopiesInput(t *testing.T) {
	r := NewRegistry()
	r.Put(&model.Pipe{Name: "old"})

	in := map[string]*model.Pipe{"new": {Name: "new"}}
	r.Replace(in)

	if _, ok := r.Get("old"); ok {
		t.Error("Replace must drop pipes not in the new set")
	}
	if _, ok := r.Get("new"); !ok {
		t.Error("Replace must install pipes from the new set")
	}

	// Mutating the caller's map afterward must not affect the registry.
	in["sneaky"] = &model.Pipe{Name: "sneaky"}
	delete(in, "new")
	if _, ok := r.Get("sneaky"); ok {
		t.Error("registry leaked a post-Replace insertion into the caller's map")
	}
	if _, ok := r.Get("new"); !ok {
		t.Error("registry lost a pipe after the caller mutated its own map")
	}
}

func names(ps []*model.Pipe) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}
