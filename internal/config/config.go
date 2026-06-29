// Package config loads TinyRaven runtime configuration. MVP: defaults overlaid
// by environment variables. The full 3-source precedence (flag > env > yaml,
// ADR 0032) lands when tr deploy/login need it; env+defaults cover Phase 1.
package config

import "os"

// Config is the resolved server configuration.
type Config struct {
	HTTPAddr     string // listen address, e.g. ":8000"
	CHHTTPURL    string // ClickHouse HTTP, e.g. "http://localhost:8123"
	CHNativeAddr string // ClickHouse native TCP, e.g. "localhost:9000"
	CHDatabase   string // target database, e.g. "tr_main"
	CHUser       string
	CHPassword   string
	RedisAddr    string // e.g. "localhost:6379"
	ProjectDir   string // dir holding .datasource/.pipe files
	AdminToken   string // bootstrap ADMIN token; empty disables bootstrap
}

// Load builds a Config from defaults overlaid by TINYBIRD_*/TR_* env vars.
func Load() Config {
	return Config{
		HTTPAddr:     env("TR_HTTP_ADDR", ":8000"),
		CHHTTPURL:    env("TR_CLICKHOUSE_HTTP", "http://localhost:8123"),
		CHNativeAddr: env("TR_CLICKHOUSE_NATIVE", "localhost:9000"),
		CHDatabase:   env("TR_CLICKHOUSE_DB", "tr_main"),
		CHUser:       env("TR_CLICKHOUSE_USER", "default"),
		CHPassword:   env("TR_CLICKHOUSE_PASSWORD", ""),
		RedisAddr:    env("TR_REDIS_ADDR", "localhost:6379"),
		ProjectDir:   env("TR_PROJECT_DIR", "."),
		AdminToken:   env("TR_ADMIN_TOKEN", ""),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
