package main

// Temporary stub wiring so `tr serve` runs before the real subsystems land.
// Deleted at integration; see TODO in serve.go.

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/tinyraven/tinyraven/internal/api"
	"github.com/tinyraven/tinyraven/internal/config"
	"github.com/tinyraven/tinyraven/internal/model"
)

func wireStubs(cfg config.Config) api.Deps {
	return api.Deps{
		Ingester:  stubIngester{},
		Pipes:     stubPipes{},
		Tokens:    stubTokens{admin: cfg.AdminToken},
		RedisPing: stubPinger{},
		CHPing:    stubPinger{},
	}
}

type stubIngester struct{}

func (stubIngester) Ingest(_ context.Context, _ string, rows []json.RawMessage) (int, int, error) {
	return len(rows), 0, nil
}

type stubPipes struct{}

func (stubPipes) Run(context.Context, string, url.Values) ([]byte, int, error) {
	return []byte(`{"error":"pipe execution not wired yet"}`), 501, nil
}

type stubTokens struct{ admin string }

func (s stubTokens) Validate(_ context.Context, value string) (*model.Token, bool, error) {
	if s.admin != "" && value == s.admin {
		return &model.Token{Name: "admin", Value: value, Scopes: []string{"ADMIN"}}, true, nil
	}
	return nil, false, nil
}

func (stubTokens) Put(context.Context, *model.Token) error { return nil }

type stubPinger struct{}

func (stubPinger) Ping(context.Context) error { return nil }
