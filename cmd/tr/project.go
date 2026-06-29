package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tinyraven/tinyraven/internal/clickhouse"
	"github.com/tinyraven/tinyraven/internal/datasource"
	"github.com/tinyraven/tinyraven/internal/model"
	"github.com/tinyraven/tinyraven/internal/pipe"
)

// project holds the parsed .datasource/.pipe files and the live registries they
// feed.
type project struct {
	ch      *clickhouse.Client
	dsReg   model.DatasourceRegistry
	pipeReg *pipe.Registry
	log     *slog.Logger
}

// apply parses every .datasource/.pipe under dir, ensures each datasource table
// exists (MVP bootstrap DDL), registers schemas in Redis, and atomically swaps
// the pipe registry (ADR 0020).
func (p *project) apply(ctx context.Context, dir string) error {
	dss, pipes, err := loadProject(dir)
	if err != nil {
		return err
	}
	for _, ds := range dss {
		if err := p.ch.EnsureTable(ctx, ds); err != nil {
			return err
		}
		if err := p.dsReg.Put(ctx, ds); err != nil {
			return err
		}
	}
	p.pipeReg.Replace(toPipeMap(pipes))
	p.log.Info("project loaded", "datasources", len(dss), "pipes", len(pipes), "dir", dir)
	return nil
}

// watch polls file mtimes and reloads on change (ADR 0020, dev-only). Pipe edits
// swap the registry instantly; datasource edits re-register the schema but do
// NOT auto-migrate — that routes through `tr deploy` (logged as a notice).
// ponytail: mtime poll is the zero-dep fallback to fsnotify (PROMPT.md).
func (p *project) watch(ctx context.Context, dir string) {
	prev := fingerprint(dir)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cur := fingerprint(dir)
			if sameFingerprint(prev, cur) {
				continue
			}
			if dsChanged(prev, cur) {
				p.log.Warn("datasource change detected; run `tr deploy` to migrate schema (no instant DDL, ADR 0020)")
			}
			prev = cur
			if err := p.apply(ctx, dir); err != nil {
				p.log.Error("hot reload failed", "err", err)
			} else {
				p.log.Info("hot reloaded project")
			}
		}
	}
}

func loadProject(dir string) ([]*model.Datasource, []*model.Pipe, error) {
	var dss []*model.Datasource
	var pipes []*model.Pipe
	walk := func(suffix string, fn func(path string) error) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
				continue
			}
			if err := fn(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(".datasource", func(path string) error {
		ds, err := datasource.ParseFile(path)
		if err != nil {
			return err
		}
		dss = append(dss, ds)
		return nil
	}); err != nil {
		return nil, nil, err
	}
	if err := walk(".pipe", func(path string) error {
		pp, err := pipe.ParseFile(path)
		if err != nil {
			return err
		}
		pipes = append(pipes, pp)
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return dss, pipes, nil
}

func toPipeMap(pipes []*model.Pipe) map[string]*model.Pipe {
	m := make(map[string]*model.Pipe, len(pipes))
	for _, p := range pipes {
		m[p.Name] = p
	}
	return m
}

// fingerprint maps each project file to "<size>:<modtime>" for change detection.
func fingerprint(dir string) map[string]string {
	fp := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fp
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !(strings.HasSuffix(n, ".datasource") || strings.HasSuffix(n, ".pipe")) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fp[n] = info.ModTime().Format(time.RFC3339Nano) + ":" + itoa(info.Size())
	}
	return fp
}

func sameFingerprint(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// dsChanged reports whether any .datasource file was added/removed/modified.
func dsChanged(prev, cur map[string]string) bool {
	keys := map[string]struct{}{}
	for k := range prev {
		keys[k] = struct{}{}
	}
	for k := range cur {
		keys[k] = struct{}{}
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if strings.HasSuffix(k, ".datasource") && prev[k] != cur[k] {
			return true
		}
	}
	return false
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
