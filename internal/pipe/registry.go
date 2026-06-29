package pipe

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Registry is an in-memory model.PipeRegistry with lock-free reads and an
// atomic snapshot swap for hot reload (ADR 0020). Get/List read the current
// snapshot without blocking writers; Put/Replace serialize via mu and publish
// a new immutable snapshot copy-on-write.
type Registry struct {
	mu   sync.Mutex // serializes writers only
	snap atomic.Pointer[map[string]*model.Pipe]
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	r := &Registry{}
	empty := map[string]*model.Pipe{}
	r.snap.Store(&empty)
	return r
}

var _ model.PipeRegistry = (*Registry)(nil)

// Get returns the pipe named name from the current snapshot.
func (r *Registry) Get(name string) (*model.Pipe, bool) {
	p, ok := (*r.snap.Load())[name]
	return p, ok
}

// List returns every pipe in the current snapshot, ordered by name.
func (r *Registry) List() []*model.Pipe {
	m := *r.snap.Load()
	out := make([]*model.Pipe, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Put inserts or replaces a single pipe, publishing a new snapshot.
func (r *Registry) Put(p *model.Pipe) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur := *r.snap.Load()
	next := make(map[string]*model.Pipe, len(cur)+1)
	for k, v := range cur {
		next[k] = v
	}
	next[p.Name] = p
	r.snap.Store(&next)
}

// Replace atomically swaps the entire pipe set in one operation — the hot-reload
// path used by tr deploy. The input map is copied so the caller may mutate it
// afterward without affecting the registry.
func (r *Registry) Replace(pipes map[string]*model.Pipe) {
	next := make(map[string]*model.Pipe, len(pipes))
	for k, v := range pipes {
		next[k] = v
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap.Store(&next)
}
