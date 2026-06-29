// Package pipestats implements model.StatsRecorder: it records per-query
// observability rows and flushes them to ClickHouse in batches via the shared
// CHInserter, mirroring the gatherer's max(N, interval) flush (ADR 0014).
// Recording is non-blocking — observability must never slow or block the query
// path, so a full buffer drops stats rather than applying backpressure.
package pipestats

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Defaults: flush on max(1000 rows, 5s); buffer up to 4096 stats before drops.
const (
	defaultFlushSize     = 1000
	defaultFlushInterval = 5 * time.Second
	defaultBufferSize    = 4096
)

// entry pairs a stat with the time it was recorded, so the flushed row's
// timestamp reflects query time rather than flush time.
type entry struct {
	stat model.QueryStat
	ts   time.Time
}

// Recorder buffers QueryStats and flushes them to ClickHouse. Build with New;
// the background flusher starts immediately. Call Close to drain on shutdown.
type Recorder struct {
	ins model.CHInserter
	ds  *model.Datasource
	log *slog.Logger

	flushSize     int
	flushInterval time.Duration

	ch        chan entry
	stop      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu      sync.Mutex
	dropped int64 // observability-of-observability: how many stats we dropped
}

// Option configures a Recorder.
type Option func(*Recorder)

// WithFlushSize sets the row count that triggers an early flush (default 1000).
func WithFlushSize(n int) Option {
	return func(r *Recorder) {
		if n > 0 {
			r.flushSize = n
		}
	}
}

// WithFlushInterval sets the max time a stat waits before flushing (default 5s).
func WithFlushInterval(d time.Duration) Option {
	return func(r *Recorder) {
		if d > 0 {
			r.flushInterval = d
		}
	}
}

// WithBufferSize sets the in-flight buffer capacity before Record drops
// (default 4096).
func WithBufferSize(n int) Option {
	return func(r *Recorder) {
		if n > 0 {
			r.ch = make(chan entry, n)
		}
	}
}

// WithLogger sets the slog logger (default: slog.Default).
func WithLogger(l *slog.Logger) Option {
	return func(r *Recorder) {
		if l != nil {
			r.log = l
		}
	}
}

// New builds a Recorder and starts its background flusher. ins writes the
// batched rows; the destination table is described by Schema().
func New(ins model.CHInserter, opts ...Option) *Recorder {
	r := &Recorder{
		ins:           ins,
		ds:            statsDatasource(),
		log:           slog.Default(),
		flushSize:     defaultFlushSize,
		flushInterval: defaultFlushInterval,
		ch:            make(chan entry, defaultBufferSize),
		stop:          make(chan struct{}),
	}
	for _, o := range opts {
		o(r)
	}
	r.wg.Add(1)
	go r.loop()
	return r
}

// Record buffers a stat for async flush. It is non-blocking: if the buffer is
// full the stat is dropped (counted), never applying backpressure to the query
// path (ADR 0014 — best-effort observability).
//
// ponytail: dropping is the intended overflow behavior; the dropped counter is
// in-memory only. A future build could expose it as a Prometheus gauge.
func (r *Recorder) Record(stat model.QueryStat) {
	select {
	case r.ch <- entry{stat: stat, ts: time.Now().UTC()}:
	default:
		r.mu.Lock()
		r.dropped++
		r.mu.Unlock()
	}
}

// Dropped returns how many stats have been dropped due to buffer overflow.
func (r *Recorder) Dropped() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dropped
}

// Schema returns the synthetic Datasource describing the pipe_stats table, so
// the orchestrator/deploy can EnsureTable it before the first flush.
//
// ponytail: Tinybird writes to db `tinybird.pipe_stats`; the MVP writes to a
// `pipe_stats` table in the active workspace database. Promote to a dedicated
// `tinybird` database when system-table parity matters.
func (r *Recorder) Schema() *model.Datasource { return r.ds }

// Close stops the flusher and drains buffered stats, returning when the final
// flush completes or ctx expires. Safe to call once.
func (r *Recorder) Close(ctx context.Context) error {
	r.closeOnce.Do(func() { close(r.stop) })
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Recorder) loop() {
	defer r.wg.Done()
	t := time.NewTicker(r.flushInterval)
	defer t.Stop()

	buf := make([]entry, 0, r.flushSize)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		r.flush(context.Background(), buf)
		buf = buf[:0]
	}

	for {
		select {
		case <-r.stop:
			// Drain whatever is still buffered, then final flush.
			for {
				select {
				case e := <-r.ch:
					buf = append(buf, e)
				default:
					flush()
					return
				}
			}
		case e := <-r.ch:
			buf = append(buf, e)
			if len(buf) >= r.flushSize {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

// flush writes a batch of stats as rows through the CHInserter. Failures are
// logged and dropped — losing observability rows is acceptable (ADR 0014).
func (r *Recorder) flush(ctx context.Context, batch []entry) {
	rows := make([]map[string]any, len(batch))
	for i, e := range batch {
		rows[i] = map[string]any{
			"pipe":        e.stat.Pipe,
			"duration_ms": e.stat.DurationMS,
			"read_rows":   e.stat.ReadRows,
			"read_bytes":  e.stat.ReadBytes,
			"status_code": e.stat.StatusCode,
			"error":       e.stat.Error,
			"timestamp":   e.ts,
		}
	}
	if err := r.ins.Insert(ctx, r.ds, rows); err != nil {
		r.log.Error("pipestats flush failed", "rows", len(rows), "err", err)
	}
}

// statsDatasource is the synthetic schema for the pipe_stats table, fed through
// the same CHInserter path as real datasources (ADR 0014 — pipe_stats is an
// internal datasource). Engine + sorting key let EnsureTable create it.
func statsDatasource() *model.Datasource {
	return &model.Datasource{
		Name: "pipe_stats",
		Schema: []model.Column{
			{Name: "pipe", Type: "String"},
			{Name: "duration_ms", Type: "Float64"},
			{Name: "read_rows", Type: "UInt64"},
			{Name: "read_bytes", Type: "UInt64"},
			{Name: "status_code", Type: "UInt16"},
			{Name: "error", Type: "String"},
			{Name: "timestamp", Type: "DateTime DEFAULT now()"},
		},
		Engine:     "MergeTree",
		EngineOpts: map[string]string{"ENGINE_SORTING_KEY": "timestamp"},
	}
}
