//go:build integration

package pipe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// httpCH is a minimal model.CHQuerier over the ClickHouse HTTP interface, used
// only by the integration test. Params (param_<name>) and settings are passed
// as CGI args; ClickHouse binds {name:Type} placeholders server-side.
type httpCH struct {
	base   string
	client *http.Client
}

func (c httpCH) Query(ctx context.Context, sql string, params, settings map[string]string) ([]byte, error) {
	q := url.Values{}
	q.Set("query", sql)
	for k, v := range params {
		q.Set(k, v)
	}
	for k, v := range settings {
		q.Set(k, v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("clickhouse status %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

// TestRun_Integration runs one end-to-end pipe query against a real ClickHouse
// at TR_CLICKHOUSE_HTTP (default http://localhost:8123). Skips if unreachable.
// Run with: go test -tags integration ./internal/pipe/...
func TestRun_Integration(t *testing.T) {
	base := os.Getenv("TR_CLICKHOUSE_HTTP")
	if base == "" {
		base = "http://localhost:8123"
	}
	ch := httpCH{base: base, client: &http.Client{Timeout: 5 * time.Second}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := ch.Query(ctx, "SELECT 1 FORMAT JSON", nil, nil); err != nil {
		t.Skipf("ClickHouse unavailable at %s: %v", base, err)
	}

	p, err := Parse("answer", "NODE endpoint\nSQL >\n    SELECT {{Int32(n)}} + 1 AS answer\nTYPE endpoint")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	reg := NewRegistry()
	reg.Put(p)
	e := NewExecutor(ch, reg, nil)

	body, status, err := e.Run(ctx, "answer", url.Values{"n": {"41"}})
	if err != nil {
		t.Fatalf("Run: %v (body=%s)", err, body)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(string(body), `"answer"`) || !strings.Contains(string(body), "42") {
		t.Fatalf("expected answer=42 in response, got: %s", body)
	}
}
