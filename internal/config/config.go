// Package config loads TinyRaven runtime configuration. Precedence, highest
// first (ADR 0032): explicit env (TINYBIRD_*/TR_*) > project .tr/config.yml >
// home ~/.tinyraven/config.yml > built-in defaults. Hand-rolled — no viper; the
// only file parser is yaml.v3 (ADR 0032).
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Config is the resolved server + CLI configuration.
type Config struct {
	HTTPAddr      string // listen address, e.g. ":8000"
	CHHTTPURL     string // ClickHouse HTTP, e.g. "http://localhost:8123"
	CHNativeAddr  string // ClickHouse native TCP, e.g. "localhost:9000"
	CHDatabase    string // target database, e.g. "tr_main"
	CHUser        string
	CHPassword    string
	RedisAddr     string // e.g. "localhost:6379"
	ProjectDir    string // dir holding .datasource/.pipe files
	AdminToken    string // bootstrap ADMIN token; empty disables bootstrap
	PipeRateLimit int    // per-token req/s on /v0/pipes (ADR 0015); 0 disables
	CHROUser      string // dedicated read-only CH user for reads (ADR 0011); empty = use CHUser
	CHROPassword  string
	DocsEnabled   bool // serve /tr/v1/docs (ADR 0017); off by default

	// Client-facing config, Tinybird-compatible (read from config.yml / env).
	Host      string // API host the `tr` CLI talks to, e.g. "http://localhost:8000"
	Token     string // bearer token for API calls
	Workspace string // active Tinybird-style workspace (deployment); branch→DB is separate
}

// FileConfig is the on-disk shape of ~/.tinyraven/config.yml — byte-compatible
// with Tinybird's ~/.tinybird/config.yml (host, token, optional workspace).
type FileConfig struct {
	Host      string `yaml:"host,omitempty"`
	Token     string `yaml:"token,omitempty"`
	Workspace string `yaml:"workspace,omitempty"`
}

const (
	// HomeConfigPath holds secrets (token); see WriteConfigFile perms.
	HomeConfigPath = "~/.tinyraven/config.yml"
	// ProjectConfigPath is non-secret project state (host/workspace), checked in.
	ProjectConfigPath = ".tr/config.yml"
)

// Load builds a Config from defaults, overlays the home then project config
// files, then overlays env vars (highest precedence). It always returns a
// usable Config for the server — missing config files are not an error.
func Load() Config {
	c := Config{
		HTTPAddr:      env("TR_HTTP_ADDR", ":8000"),
		CHHTTPURL:     env("TR_CLICKHOUSE_HTTP", "http://localhost:8123"),
		CHNativeAddr:  env("TR_CLICKHOUSE_NATIVE", "localhost:9000"),
		CHDatabase:    env("TR_CLICKHOUSE_DB", "tr_main"),
		CHUser:        env("TR_CLICKHOUSE_USER", "default"),
		CHPassword:    env("TR_CLICKHOUSE_PASSWORD", ""),
		RedisAddr:     env("TR_REDIS_ADDR", "localhost:6379"),
		ProjectDir:    env("TR_PROJECT_DIR", "."),
		AdminToken:    env("TR_ADMIN_TOKEN", ""),
		PipeRateLimit: envInt("TR_PIPE_RATE_LIMIT", 100),
		CHROUser:      env("TR_CLICKHOUSE_RO_USER", ""),
		CHROPassword:  env("TR_CLICKHOUSE_RO_PASSWORD", ""),
		DocsEnabled:   os.Getenv("TR_DOCS_ENABLED") == "true",

		Host:      "http://localhost:8000",
		Token:     "",
		Workspace: "",
	}

	// File layer: home first, then project overrides home (ADR 0032).
	c.applyFile(loadFileSoft(HomeConfigPath))
	c.applyFile(loadFileSoft(ProjectConfigPath))

	// Env layer (highest): TINYBIRD_* preferred for parity, TR_* as native alias.
	c.Host = envOr(c.Host, "TINYBIRD_HOST", "TR_HOST")
	c.Token = envOr(c.Token, "TINYBIRD_TOKEN", "TR_TOKEN")
	c.Workspace = envOr(c.Workspace, "TINYBIRD_WORKSPACE", "TR_WORKSPACE")
	return c
}

// LoadConfigFile reads and parses a config.yml. A leading "~" is resolved via
// os.UserHomeDir. Missing-file and parse errors are returned to the caller (a
// future `tr login` distinguishes them); Load swallows missing files itself.
func LoadConfigFile(path string) (FileConfig, error) {
	var fc FileConfig
	b, err := os.ReadFile(expandHome(path))
	if err != nil {
		return fc, err
	}
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return fc, fmt.Errorf("parse %s: %w", path, err)
	}
	return fc, nil
}

// WriteConfigFile writes c to path as YAML, creating parent dirs. The file holds
// a token, so it is written 0600 under a 0700 directory (for `tr login`).
func WriteConfigFile(path string, c FileConfig) error {
	path = expandHome(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// applyFile overlays non-empty fields from fc onto c (zero values never clobber
// an already-resolved value).
func (c *Config) applyFile(fc FileConfig) {
	if fc.Host != "" {
		c.Host = fc.Host
	}
	if fc.Token != "" {
		c.Token = fc.Token
	}
	if fc.Workspace != "" {
		c.Workspace = fc.Workspace
	}
}

// loadFileSoft reads a config file for Load: a missing file is a no-op, a parse
// error is logged and skipped (defaults still apply).
func loadFileSoft(path string) FileConfig {
	fc, err := LoadConfigFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("config: skipping unreadable config file", "path", path, "err", err)
		}
		return FileConfig{}
	}
	return fc
}

// expandHome resolves a leading "~" / "~/" to the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt reads an integer env var, falling back to def on unset/invalid.
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// envOr returns the first non-empty env var among keys, else cur.
func envOr(cur string, keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return cur
}
