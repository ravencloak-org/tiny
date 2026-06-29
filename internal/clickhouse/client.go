// Package clickhouse is TinyRaven's ClickHouse adapter. One Client drives both
// transports ClickHouse exposes (ADR 0013): the HTTP interface (8123) for
// read-only queries + liveness, and the native protocol (TCP 9000) for the
// batched insert hot path. It implements model.CHQuerier, model.CHPinger and
// model.CHInserter.
package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/tinyraven/tinyraven/internal/model"
)

// Config is the connection info for both transports. Populated by the
// orchestrator from internal/config.
type Config struct {
	HTTPURL    string // ClickHouse HTTP, e.g. "http://localhost:8123"
	NativeAddr string // native TCP, e.g. "localhost:9000"; empty disables Insert
	Database   string // target database
	User       string
	Password   string
}

// Client talks to one ClickHouse instance/database over both transports.
type Client struct {
	httpURL  string
	db       string
	user     string
	password string
	http     *http.Client
	conn     driver.Conn // native; nil when NativeAddr is empty
}

// CHError carries a ClickHouse exception code (X-ClickHouse-Exception-Code) so
// the API layer can map it to a Tinybird-compatible status (ADR 0012).
type CHError struct {
	Code int
	Msg  string
}

func (e *CHError) Error() string { return fmt.Sprintf("clickhouse exception %d: %s", e.Code, e.Msg) }

// New builds a Client. The native conn is opened lazily by the driver (no dial
// here), so New only fails on bad options; readiness is probed via Ping.
func New(cfg Config) (*Client, error) {
	c := &Client{
		httpURL:  strings.TrimRight(cfg.HTTPURL, "/"),
		db:       cfg.Database,
		user:     cfg.User,
		password: cfg.Password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
	if cfg.NativeAddr != "" {
		conn, err := clickhouse.Open(&clickhouse.Options{
			Addr: []string{cfg.NativeAddr},
			Auth: clickhouse.Auth{Database: cfg.Database, Username: cfg.User, Password: cfg.Password},
		})
		if err != nil {
			return nil, err
		}
		c.conn = conn
	}
	return c, nil
}

// Close releases the native connection pool.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Query runs sql over the HTTP interface and returns the response body verbatim
// (ADR 0013). params (already prefixed param_<name> by the caller) and settings
// are forwarded as URL query args; the target db and credentials come from cfg.
func (c *Client) Query(ctx context.Context, sql string, params, settings map[string]string) ([]byte, error) {
	u, err := url.Parse(c.httpURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("database", c.db)
	for k, v := range params {
		q.Set(k, v)
	}
	for k, v := range settings {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(sql))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-ClickHouse-User", c.user)
	if c.password != "" {
		req.Header.Set("X-ClickHouse-Key", c.password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		code, _ := strconv.Atoi(resp.Header.Get("X-ClickHouse-Exception-Code"))
		return nil, &CHError{Code: code, Msg: strings.TrimSpace(string(body))}
	}
	return body, nil
}

// Ping issues SELECT 1 over HTTP for the readiness probe (ADR 0024).
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Query(ctx, "SELECT 1", nil, nil)
	return err
}

// Insert sends rows as one native batch into <db>.<ds.Name> (ADR 0013, the perf
// path). Values are appended in ds.Schema column order, coerced from their
// JSON-decoded Go types to what the native driver expects.
func (c *Client) Insert(ctx context.Context, ds *model.Datasource, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	if c.conn == nil {
		return fmt.Errorf("clickhouse: native transport not configured")
	}
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO "+c.db+"."+ds.Name)
	if err != nil {
		return err
	}
	defer batch.Close() // no-op once Send succeeds; cleans up if we return early

	vals := make([]any, len(ds.Schema))
	for _, row := range rows {
		for i, col := range ds.Schema {
			vals[i] = coerceValue(col.Type, row[col.Name]) // missing field -> nil
		}
		if err := batch.Append(vals...); err != nil {
			return err
		}
	}
	return batch.Send()
}

// coerceValue maps an encoding/json value to the Go type the native driver
// wants for a ClickHouse column. encoding/json gives numbers as float64, so
// integer columns need narrowing; DateTime wants time.Time; object-valued
// String/JSON columns are re-marshalled to a JSON string.
//
// ponytail: pragmatic type ceiling — Int128/256, Decimal, Array/Map/Tuple and
// other composites pass through untouched and rely on the driver. Widen here
// when a real schema needs it.
func coerceValue(chType string, v any) any {
	if v == nil {
		return nil
	}
	t := baseCHType(chType)
	switch {
	case strings.HasPrefix(t, "Int"), strings.HasPrefix(t, "UInt"):
		return coerceInt(t, v)
	case t == "Float32":
		if f, ok := toFloat(v); ok {
			return float32(f)
		}
	case t == "Float64":
		if f, ok := toFloat(v); ok {
			return f
		}
	case strings.HasPrefix(t, "DateTime"), t == "Date", t == "Date32":
		return coerceTime(v)
	case t == "JSON" || t == "String" || strings.HasPrefix(t, "FixedString"):
		return coerceString(v)
	}
	return v // Bool, enums, composites: trust the driver
}

// baseCHType strips Nullable()/LowCardinality() wrappers to the inner type.
func baseCHType(t string) string {
	t = strings.TrimSpace(t)
	for _, w := range []string{"Nullable", "LowCardinality"} {
		if strings.HasPrefix(t, w+"(") && strings.HasSuffix(t, ")") {
			return baseCHType(t[len(w)+1 : len(t)-1])
		}
	}
	return t
}

func coerceInt(t string, v any) any {
	f, ok := toFloat(v)
	if !ok {
		return v
	}
	n := int64(f)
	switch t {
	case "Int8":
		return int8(n)
	case "Int16":
		return int16(n)
	case "Int32":
		return int32(n)
	case "Int64":
		return n
	case "UInt8":
		return uint8(n)
	case "UInt16":
		return uint16(n)
	case "UInt32":
		return uint32(n)
	case "UInt64":
		return uint64(n)
	}
	return v // Int128/256 etc — ponytail ceiling
}

func coerceTime(v any) any {
	if s, ok := v.(string); ok {
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			return ts
		}
		return v
	}
	if f, ok := toFloat(v); ok {
		return time.Unix(int64(f), 0).UTC()
	}
	return v // already a time.Time (e.g. quarantine timestamp) or unknown: pass through
}

func coerceString(v any) any {
	switch v.(type) {
	case string:
		return v
	case map[string]any, []any:
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return v
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
