package gatherer

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

// fakeInserter records every batch and signals each call on a channel so tests
// can wait for an async flush without sleeping.
type fakeInserter struct {
	mu        sync.Mutex
	byTable   map[string][]map[string]any
	calls     int
	flushed   chan struct{}
	insertErr error
}

func newFakeInserter() *fakeInserter {
	return &fakeInserter{byTable: map[string][]map[string]any{}, flushed: make(chan struct{}, 64)}
}

func (f *fakeInserter) Insert(_ context.Context, ds *model.Datasource, rows []map[string]any) error {
	f.mu.Lock()
	f.calls++
	if f.insertErr == nil {
		f.byTable[ds.Name] = append(f.byTable[ds.Name], rows...)
	}
	f.mu.Unlock()
	f.flushed <- struct{}{}
	return f.insertErr
}

func (f *fakeInserter) count(table string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.byTable[table])
}

// fakeRegistry is an in-memory DatasourceRegistry.
type fakeRegistry struct{ ds map[string]*model.Datasource }

func newFakeRegistry(dss ...*model.Datasource) *fakeRegistry {
	m := map[string]*model.Datasource{}
	for _, d := range dss {
		m[d.Name] = d
	}
	return &fakeRegistry{ds: m}
}

func (r *fakeRegistry) Get(_ context.Context, name string) (*model.Datasource, bool, error) {
	d, ok := r.ds[name]
	return d, ok, nil
}
func (r *fakeRegistry) Put(_ context.Context, d *model.Datasource) error {
	r.ds[d.Name] = d
	return nil
}
func (r *fakeRegistry) List(_ context.Context) ([]*model.Datasource, error) {
	out := make([]*model.Datasource, 0, len(r.ds))
	for _, d := range r.ds {
		out = append(out, d)
	}
	return out, nil
}

func eventsDS() *model.Datasource {
	return &model.Datasource{
		Name:   "events",
		Schema: []model.Column{{Name: "id", Type: "UInt64"}, {Name: "name", Type: "String"}},
	}
}

func raws(t *testing.T, objs ...any) []json.RawMessage {
	t.Helper()
	out := make([]json.RawMessage, len(objs))
	for i, o := range objs {
		switch v := o.(type) {
		case string: // pre-formed raw (used for deliberately bad JSON)
			out[i] = json.RawMessage(v)
		default:
			b, err := json.Marshal(o)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			out[i] = b
		}
	}
	return out
}

// waitFlush blocks until at least one Insert call lands or the deadline passes.
func waitFlush(t *testing.T, f *fakeInserter, d time.Duration) {
	t.Helper()
	select {
	case <-f.flushed:
	case <-time.After(d):
		t.Fatalf("timed out waiting for flush")
	}
}

func TestIngestValidRowsFlushOnClose(t *testing.T) {
	ins := newFakeInserter()
	g := New(ins, newFakeRegistry(eventsDS()), WithFlushSize(1000), WithFlushInterval(time.Hour))

	ok, q, err := g.Ingest(context.Background(), "events",
		raws(t, map[string]any{"id": 1, "name": "a"}, map[string]any{"id": 2, "name": "b"}))
	if err != nil || ok != 2 || q != 0 {
		t.Fatalf("Ingest = (%d,%d,%v), want (2,0,nil)", ok, q, err)
	}

	if err := g.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := ins.count("events"); got != 2 {
		t.Fatalf("flushed %d rows, want 2", got)
	}
}

func TestIngestFlushOnSizeThreshold(t *testing.T) {
	ins := newFakeInserter()
	g := New(ins, newFakeRegistry(eventsDS()), WithFlushSize(3), WithFlushInterval(time.Hour))
	defer g.Close(context.Background())

	// Three rows hits the threshold and triggers the flusher (not via timeout).
	if _, _, err := g.Ingest(context.Background(), "events",
		raws(t, map[string]any{"id": 1}, map[string]any{"id": 2}, map[string]any{"id": 3})); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFlush(t, ins, 2*time.Second)
	if got := ins.count("events"); got != 3 {
		t.Fatalf("flushed %d rows, want 3", got)
	}
}

func TestIngestFlushOnTimeout(t *testing.T) {
	ins := newFakeInserter()
	g := New(ins, newFakeRegistry(eventsDS()), WithFlushSize(1000), WithFlushInterval(50*time.Millisecond))
	defer g.Close(context.Background())

	if _, _, err := g.Ingest(context.Background(), "events", raws(t, map[string]any{"id": 1})); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	waitFlush(t, ins, 2*time.Second) // below flushSize: only the ticker can flush it
	if got := ins.count("events"); got != 1 {
		t.Fatalf("flushed %d rows, want 1", got)
	}
}

func TestIngestBadJSONQuarantined(t *testing.T) {
	ins := newFakeInserter()
	g := New(ins, newFakeRegistry(eventsDS()), WithFlushSize(1000), WithFlushInterval(time.Hour))

	// A bare number, a JSON array, and null are all not-an-object -> quarantine.
	ok, q, err := g.Ingest(context.Background(), "events",
		raws(t, map[string]any{"id": 1}, "123", "[1,2]", "null"))
	if err != nil {
		t.Fatalf("Ingest err: %v", err)
	}
	if ok != 1 || q != 3 {
		t.Fatalf("got (successful=%d, quarantined=%d), want (1,3)", ok, q)
	}

	g.Close(context.Background())
	if got := ins.count("events"); got != 1 {
		t.Fatalf("events rows = %d, want 1", got)
	}
	if got := ins.count("events_quarantine"); got != 3 {
		t.Fatalf("quarantine rows = %d, want 3", got)
	}
}

func TestIngestUnknownDatasource(t *testing.T) {
	ins := newFakeInserter()
	g := New(ins, newFakeRegistry(eventsDS()))
	defer g.Close(context.Background())

	ok, q, err := g.Ingest(context.Background(), "nope", raws(t, map[string]any{"id": 1}))
	if err == nil {
		t.Fatal("expected error for unknown datasource")
	}
	if ok != 0 || q != 0 {
		t.Fatalf("got (%d,%d), want (0,0)", ok, q)
	}
	if ins.calls != 0 {
		t.Fatalf("inserter called %d times, want 0", ins.calls)
	}
}

func TestConcurrentIngestNoLoss(t *testing.T) {
	ins := newFakeInserter()
	g := New(ins, newFakeRegistry(eventsDS()), WithFlushSize(1000), WithFlushInterval(5*time.Millisecond))

	const goroutines, perG = 16, 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				if _, _, err := g.Ingest(context.Background(), "events",
					raws(t, map[string]any{"id": base + j})); err != nil {
					t.Errorf("Ingest: %v", err)
				}
			}
		}(i * perG)
	}
	wg.Wait()
	if err := g.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := ins.count("events"); got != goroutines*perG {
		t.Fatalf("flushed %d rows, want %d", got, goroutines*perG)
	}
}

func TestValidateRow(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"object", `{"a":1}`, false},
		{"empty object", `{}`, false},
		{"number", `42`, true},
		{"array", `[1,2,3]`, true},
		{"null", `null`, true},
		{"garbage", `{not json`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateRow(json.RawMessage(tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateRow(%s) err=%v, wantErr=%v", tc.raw, err, tc.wantErr)
			}
		})
	}
}

func TestFlushDropsOnInsertError(t *testing.T) {
	ins := newFakeInserter()
	ins.insertErr = errors.New("ch down")
	g := New(ins, newFakeRegistry(eventsDS()), WithFlushSize(1), WithFlushInterval(time.Hour))
	defer g.Close(context.Background())

	if _, _, err := g.Ingest(context.Background(), "events", raws(t, map[string]any{"id": 1})); err != nil {
		t.Fatalf("Ingest: %v", err) // ack succeeds even though the flush will fail
	}
	waitFlush(t, ins, 2*time.Second)
	if got := ins.count("events"); got != 0 {
		t.Fatalf("recorded %d rows despite insert error, want 0", got)
	}
}
