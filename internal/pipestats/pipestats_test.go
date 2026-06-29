package pipestats

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

// fakeInserter records flushed rows. An optional gate blocks the first Insert
// so tests can force buffer overflow deterministically. Thread-safe for -race.
type fakeInserter struct {
	mu     sync.Mutex
	rows   []map[string]any
	gate   chan struct{} // if non-nil, first Insert signals then blocks on release
	signal chan struct{}
	once   sync.Once
}

func (f *fakeInserter) Insert(_ context.Context, _ *model.Datasource, rows []map[string]any) error {
	if f.gate != nil {
		f.once.Do(func() {
			close(f.signal) // tell the test the flusher is here
			<-f.gate        // block until released
		})
	}
	f.mu.Lock()
	f.rows = append(f.rows, rows...)
	f.mu.Unlock()
	return nil
}

func (f *fakeInserter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// waitFor polls fn until true or the deadline elapses.
func waitFor(t *testing.T, d time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", d)
}

func TestFlushOnSize(t *testing.T) {
	ins := &fakeInserter{}
	r := New(ins, WithFlushSize(2), WithFlushInterval(time.Hour))
	defer r.Close(context.Background())

	r.Record(model.QueryStat{Pipe: "a", StatusCode: 200})
	r.Record(model.QueryStat{Pipe: "b", StatusCode: 200})

	waitFor(t, time.Second, func() bool { return ins.count() == 2 })

	ins.mu.Lock()
	defer ins.mu.Unlock()
	if ins.rows[0]["pipe"] != "a" || ins.rows[1]["pipe"] != "b" {
		t.Fatalf("rows out of order/wrong: %v", ins.rows)
	}
	if _, ok := ins.rows[0]["timestamp"].(time.Time); !ok {
		t.Fatalf("timestamp not set as time.Time: %v", ins.rows[0]["timestamp"])
	}
}

func TestFlushOnClose(t *testing.T) {
	ins := &fakeInserter{}
	r := New(ins, WithFlushSize(1000), WithFlushInterval(time.Hour))

	r.Record(model.QueryStat{Pipe: "drain", StatusCode: 200})
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if ins.count() != 1 {
		t.Fatalf("Close should drain 1 buffered stat, got %d", ins.count())
	}
}

func TestRecordNonBlockingDrops(t *testing.T) {
	ins := &fakeInserter{gate: make(chan struct{}), signal: make(chan struct{})}
	// flushSize 1 -> first Record triggers a flush that blocks in Insert;
	// bufferSize 1 -> only one more stat fits before Record must drop.
	r := New(ins, WithFlushSize(1), WithBufferSize(1), WithFlushInterval(time.Hour))

	r.Record(model.QueryStat{Pipe: "first"})
	<-ins.signal // flusher is now blocked inside Insert

	for i := 0; i < 20; i++ {
		r.Record(model.QueryStat{Pipe: "flood"}) // must never block
	}
	if r.Dropped() == 0 {
		t.Fatal("expected drops while flusher blocked, got 0")
	}

	close(ins.gate) // release the flusher
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSchema(t *testing.T) {
	r := New(&fakeInserter{}, WithFlushInterval(time.Hour))
	defer r.Close(context.Background())

	ds := r.Schema()
	if ds.Name != "pipe_stats" {
		t.Fatalf("schema name = %q", ds.Name)
	}
	if ds.Engine != "MergeTree" || ds.EngineOpts["ENGINE_SORTING_KEY"] != "timestamp" {
		t.Fatalf("schema engine/opts wrong: %q %v", ds.Engine, ds.EngineOpts)
	}
	want := []string{"pipe", "duration_ms", "read_rows", "read_bytes", "status_code", "error", "timestamp"}
	if len(ds.Schema) != len(want) {
		t.Fatalf("schema cols = %d, want %d", len(ds.Schema), len(want))
	}
	for i, c := range ds.Schema {
		if c.Name != want[i] {
			t.Fatalf("col[%d] = %q, want %q", i, c.Name, want[i])
		}
	}
}
