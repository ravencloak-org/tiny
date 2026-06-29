// Package gatherer buffers incoming events in-process and flushes them to
// ClickHouse in batches (ADRs 0004, 0018). It implements model.Ingester:
// Ingest validates each row, quarantines the bad ones, buffers the good ones,
// and acks immediately (the API returns 202) — a single background flusher
// drains the buffer on max(flushSize rows, flushInterval). On graceful Close
// the remaining buffer is drained so nothing is lost on clean shutdown.
package gatherer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Defaults flush on max(10k rows, 5s) per ADR 0004.
const (
	defaultFlushSize     = 10000
	defaultFlushInterval = 5 * time.Second
)

// pending is one table's buffered rows plus the datasource needed to type the
// native insert. The key in Gatherer.buffers is the destination table name.
type pending struct {
	ds   *model.Datasource
	rows []map[string]any
}

// Gatherer is the in-process event buffer + flusher.
type Gatherer struct {
	ins model.CHInserter
	reg model.DatasourceRegistry
	log *slog.Logger

	flushSize     int
	flushInterval time.Duration

	mu      sync.Mutex
	buffers map[string]*pending

	trigger   chan struct{} // size-threshold wakeups (coalesced)
	stop      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// Option configures a Gatherer.
type Option func(*Gatherer)

// WithFlushSize sets the row count that triggers an early flush (default 10000).
func WithFlushSize(n int) Option {
	return func(g *Gatherer) {
		if n > 0 {
			g.flushSize = n
		}
	}
}

// WithFlushInterval sets the max time a row waits before flushing (default 5s).
func WithFlushInterval(d time.Duration) Option {
	return func(g *Gatherer) {
		if d > 0 {
			g.flushInterval = d
		}
	}
}

// WithLogger sets the slog logger (default: slog.Default).
func WithLogger(l *slog.Logger) Option {
	return func(g *Gatherer) {
		if l != nil {
			g.log = l
		}
	}
}

// New builds a Gatherer and starts its background flusher. Call Close to drain.
func New(ins model.CHInserter, reg model.DatasourceRegistry, opts ...Option) *Gatherer {
	g := &Gatherer{
		ins:           ins,
		reg:           reg,
		log:           slog.Default(),
		flushSize:     defaultFlushSize,
		flushInterval: defaultFlushInterval,
		buffers:       make(map[string]*pending),
		trigger:       make(chan struct{}, 1),
		stop:          make(chan struct{}),
	}
	for _, o := range opts {
		o(g)
	}
	g.wg.Add(1)
	go g.loop()
	return g
}

// Ingest validates rows for a datasource and buffers them (ack-on-buffer, ADR
// 0004). Bad rows are quarantined rather than rejecting the batch (ADR 0018);
// an unknown datasource is the only per-call error (the API maps it to 4xx).
func (g *Gatherer) Ingest(ctx context.Context, datasource string, rows []json.RawMessage) (successful, quarantined int, err error) {
	ds, ok, err := g.reg.Get(ctx, datasource)
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		return 0, 0, fmt.Errorf("%w: %q", model.ErrUnknownDatasource, datasource)
	}

	valid := make([]map[string]any, 0, len(rows))
	var bad []map[string]any
	for _, raw := range rows {
		m, verr := validateRow(raw)
		if verr != nil {
			bad = append(bad, quarantineRow(raw, verr))
			continue
		}
		valid = append(valid, m)
	}

	g.mu.Lock()
	g.appendLocked(ds, valid)
	if len(bad) > 0 {
		g.appendLocked(quarantineDS(ds.QuarantineTable()), bad)
	}
	full := g.anyFullLocked()
	g.mu.Unlock()

	if full {
		g.signal()
	}
	return len(valid), len(bad), nil
}

// Close stops the flusher and drains the buffer, returning when the final flush
// completes or ctx expires (graceful drain, ADR 0004). Safe to call once.
func (g *Gatherer) Close(ctx context.Context) error {
	g.closeOnce.Do(func() { close(g.stop) })
	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *Gatherer) loop() {
	defer g.wg.Done()
	t := time.NewTicker(g.flushInterval)
	defer t.Stop()
	for {
		select {
		case <-g.stop:
			g.flushAll(context.Background()) // final drain
			return
		case <-g.trigger:
			g.flushAll(context.Background())
		case <-t.C:
			g.flushAll(context.Background())
		}
	}
}

// flushAll swaps out the whole buffer under lock, then inserts each table's
// batch outside the lock so ingest goroutines never block on ClickHouse.
func (g *Gatherer) flushAll(ctx context.Context) {
	g.mu.Lock()
	if len(g.buffers) == 0 {
		g.mu.Unlock()
		return
	}
	batches := g.buffers
	g.buffers = make(map[string]*pending)
	g.mu.Unlock()

	for _, p := range batches {
		if err := g.ins.Insert(ctx, p.ds, p.rows); err != nil {
			// ponytail: MVP drops a failed batch (logged). Durable retry / WAL is
			// a later ADR; ack-on-buffer already accepts at-most-once on crash.
			g.log.Error("gatherer flush failed", "table", p.ds.Name, "rows", len(p.rows), "err", err)
		}
	}
}

func (g *Gatherer) appendLocked(ds *model.Datasource, rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	p := g.buffers[ds.Name]
	if p == nil {
		p = &pending{ds: ds}
		g.buffers[ds.Name] = p
	}
	p.rows = append(p.rows, rows...)
}

func (g *Gatherer) anyFullLocked() bool {
	for _, p := range g.buffers {
		if len(p.rows) >= g.flushSize {
			return true
		}
	}
	return false
}

// signal wakes the flusher without blocking; a pending wakeup is enough.
func (g *Gatherer) signal() {
	select {
	case g.trigger <- struct{}{}:
	default:
	}
}

// validateRow parses one raw event. MVP validation (ADR 0018): it must decode
// as a JSON object. Schema-on-write is intentionally NOT done here (ADR 0008) —
// type coercion happens at insert time in the clickhouse package.
func validateRow(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("row is JSON null, expected an object")
	}
	return m, nil
}

// quarantineDS is the synthetic schema for a quarantine table (ADR 0018):
// {raw String, error String, timestamp DateTime}. name is already the
// quarantine table name (Datasource.QuarantineTable()).
//
// ponytail: quarantine rows ride the same CHInserter path as real data via this
// synthetic Datasource — no separate DDL/writer. Assumes the table exists with
// this shape; creation is handled by deploy, not here.
func quarantineDS(name string) *model.Datasource {
	return &model.Datasource{
		Name: name,
		Schema: []model.Column{
			{Name: "raw", Type: "String"},
			{Name: "error", Type: "String"},
			{Name: "timestamp", Type: "DateTime"},
		},
	}
}

func quarantineRow(raw json.RawMessage, cause error) map[string]any {
	return map[string]any{
		"raw":       string(raw),
		"error":     cause.Error(),
		"timestamp": time.Now().UTC(),
	}
}
