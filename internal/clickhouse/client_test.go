package clickhouse

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/tinyraven/tinyraven/internal/model"
)

func TestQueryForwardsSQLParamsSettingsAndHeaders(t *testing.T) {
	var (
		gotMethod, gotBody, gotUser, gotKey string
		gotQuery                            url.Values
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotUser = r.Header.Get("X-ClickHouse-User")
		gotKey = r.Header.Get("X-ClickHouse-Key")
		gotQuery = r.URL.Query()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c, err := New(Config{HTTPURL: srv.URL, Database: "tr_main", User: "neo", Password: "trinity"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body, err := c.Query(context.Background(), "SELECT {{p}}",
		map[string]string{"param_p": "7"}, map[string]string{"use_query_cache": "1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotBody != "SELECT {{p}}" {
		t.Errorf("body = %q, want the SQL verbatim", gotBody)
	}
	if gotUser != "neo" || gotKey != "trinity" {
		t.Errorf("auth headers = (%q,%q), want (neo,trinity)", gotUser, gotKey)
	}
	if got := gotQuery.Get("database"); got != "tr_main" {
		t.Errorf("database = %q, want tr_main", got)
	}
	if got := gotQuery.Get("param_p"); got != "7" {
		t.Errorf("param_p = %q, want 7", got)
	}
	if got := gotQuery.Get("use_query_cache"); got != "1" {
		t.Errorf("use_query_cache = %q, want 1", got)
	}
	if string(body) != `{"data":[]}` {
		t.Errorf("body passthrough = %q", body)
	}
}

func TestQueryNon200YieldsCHError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-ClickHouse-Exception-Code", "60")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Code: 60. DB::Exception: Unknown table"))
	}))
	defer srv.Close()

	c, _ := New(Config{HTTPURL: srv.URL, Database: "tr_main", User: "default"})
	_, err := c.Query(context.Background(), "SELECT 1", nil, nil)
	if err == nil {
		t.Fatal("expected error on non-200")
	}
	var che *CHError
	if !errors.As(err, &che) {
		t.Fatalf("err type = %T, want *CHError", err)
	}
	if che.Code != 60 {
		t.Errorf("CHError.Code = %d, want 60", che.Code)
	}
	if che.Msg == "" {
		t.Error("CHError.Msg should include the response body")
	}
}

func TestQueryOmitsEmptyPasswordHeader(t *testing.T) {
	var hadKeyHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadKeyHeader = r.Header["X-Clickhouse-Key"]
		_, _ = w.Write([]byte("1"))
	}))
	defer srv.Close()

	c, _ := New(Config{HTTPURL: srv.URL, Database: "tr_main", User: "default"})
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if hadKeyHeader {
		t.Error("X-ClickHouse-Key set despite empty password")
	}
}

func TestBaseCHType(t *testing.T) {
	tests := map[string]string{
		"String":                           "String",
		" UInt64 ":                         "UInt64",
		"Nullable(Int32)":                  "Int32",
		"LowCardinality(String)":           "String",
		"LowCardinality(Nullable(String))": "String",
		"DateTime('UTC')":                  "DateTime('UTC')",
	}
	for in, want := range tests {
		if got := baseCHType(in); got != want {
			t.Errorf("baseCHType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoerceInt(t *testing.T) {
	// encoding/json hands integers in as float64.
	if got := coerceValue("Int64", float64(42)); got != int64(42) {
		t.Errorf("Int64 got %v (%T), want int64(42)", got, got)
	}
	if got := coerceValue("UInt8", float64(255)); got != uint8(255) {
		t.Errorf("UInt8 got %v (%T), want uint8(255)", got, got)
	}
	if got := coerceValue("Nullable(Int32)", float64(-5)); got != int32(-5) {
		t.Errorf("Nullable(Int32) got %v (%T), want int32(-5)", got, got)
	}
	if got := coerceValue("Int64", nil); got != nil {
		t.Errorf("nil should map to nil, got %v", got)
	}
}

func TestCoerceTime(t *testing.T) {
	const rfc = "2026-06-29T12:00:00Z"
	got := coerceValue("DateTime", rfc)
	ts, ok := got.(time.Time)
	if !ok {
		t.Fatalf("DateTime string got %T, want time.Time", got)
	}
	if !ts.Equal(time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("parsed time = %v", ts)
	}

	// Epoch number -> time.Time.
	got = coerceValue("DateTime", float64(1000))
	if ts, ok := got.(time.Time); !ok || !ts.Equal(time.Unix(1000, 0).UTC()) {
		t.Errorf("epoch coercion = %v (%T)", got, got)
	}
}

func TestCoerceStringObjectToJSON(t *testing.T) {
	obj := map[string]any{"k": "v"}
	got := coerceValue("String", obj)
	if got != `{"k":"v"}` {
		t.Errorf("object->JSON string = %q", got)
	}
	if got := coerceValue("String", "plain"); got != "plain" {
		t.Errorf("string passthrough = %q", got)
	}
	if got := coerceValue("JSON", []any{1.0, 2.0}); got != "[1,2]" {
		t.Errorf("array->JSON string = %q", got)
	}
}

func TestInsertNoNativeTransport(t *testing.T) {
	c, _ := New(Config{HTTPURL: "http://localhost:8123", Database: "tr_main"}) // NativeAddr empty
	ds := &model.Datasource{Name: "events", Schema: []model.Column{{Name: "id", Type: "UInt64"}}}
	err := c.Insert(context.Background(), ds, []map[string]any{{"id": 1.0}})
	if err == nil {
		t.Fatal("expected error when native transport is not configured")
	}
}
