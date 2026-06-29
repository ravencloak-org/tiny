// Package sqlproxy serves GET/POST /v0/sql — a read-only ClickHouse SQL proxy.
// It does NOT parse SQL to enforce read-only; it forwards the query with a
// readonly profile + resource caps and lets ClickHouse refuse writes/DDL
// structurally (ADR 0011). Errors map to the shared Tinybird envelope, passing
// the ClickHouse exception code through X-DB-Exception-Code (ADR 0012).
package sqlproxy

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/tinyraven/tinyraven/internal/apierr"
	"github.com/tinyraven/tinyraven/internal/clickhouse"
	"github.com/tinyraven/tinyraven/internal/model"
)

// Default resource caps applied to every /v0/sql query. readonly=2 forbids
// writes, DDL and settings changes; the row/time caps bound a single query's
// blast radius (ADR 0011 — DoS mitigation comes free with the profile).
const (
	defaultMaxResultRows    = "100000"
	defaultMaxExecutionTime = "30" // seconds
)

// maxBodyBytes caps the SQL read from a POST body (1 MiB is generous for a
// query string and guards against an unbounded body read).
const maxBodyBytes = 1 << 20

// formatClause detects a trailing `FORMAT <name>` so we only append a default
// when the caller hasn't asked for one.
//
// ponytail: anchored, naive detection — FORMAT is always the last clause in
// ClickHouse, so anchoring to end-of-string is safe in practice. Swap for a
// real lexer only if a pathological query ever false-matches.
var formatClause = regexp.MustCompile(`(?is)\bformat\s+[a-z0-9_]+\s*;?\s*$`)

// Option configures the handler.
type Option func(*handler)

type handler struct {
	q        model.CHQuerier
	settings map[string]string
}

// WithSettings overlays caller settings onto the readonly + caps defaults.
func WithSettings(s map[string]string) Option {
	return func(h *handler) {
		for k, v := range s {
			h.settings[k] = v
		}
	}
}

// New returns the /v0/sql handler. q runs the query over ClickHouse HTTP.
//
// ponytail: MVP enforces read-only by sending readonly=2 as a per-query
// setting. Production should point q at a dedicated CH user whose profile pins
// readonly=2 server-side, so a settings injection in the query text can't unset
// it (ADR 0011 — "two ClickHouse identities").
func New(q model.CHQuerier, opts ...Option) http.Handler {
	h := &handler{
		q: q,
		settings: map[string]string{
			"readonly":           "2",
			"max_result_rows":    defaultMaxResultRows,
			"max_execution_time": defaultMaxExecutionTime,
		},
	}
	for _, o := range opts {
		o(h)
	}
	return h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sql, err := extractSQL(r)
	if err != nil {
		apierr.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(sql) == "" {
		apierr.WriteError(w, http.StatusBadRequest, "missing required parameter: q")
		return
	}

	if !formatClause.MatchString(sql) {
		sql = strings.TrimRight(sql, "; \t\r\n") + " FORMAT JSON"
	}

	body, qerr := h.q.Query(r.Context(), sql, nil, h.settings)
	if qerr != nil {
		var che *clickhouse.CHError
		if errors.As(qerr, &che) {
			apierr.WriteErrorWithCode(w, mapStatus(che.Code), che.Code, che.Msg)
			return
		}
		apierr.WriteError(w, http.StatusInternalServerError, qerr.Error())
		return
	}
	apierr.WriteJSON(w, http.StatusOK, body)
}

// extractSQL pulls the query from ?q= (GET or POST query string), then from a
// POST body — form-encoded (q=...) or a raw SQL body.
func extractSQL(r *http.Request) (string, error) {
	if q := r.URL.Query().Get("q"); q != "" {
		return q, nil
	}
	if r.Method != http.MethodPost || r.Body == nil {
		return "", nil
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return "", errors.New("could not read request body")
	}
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if vals, perr := url.ParseQuery(s); perr == nil {
			if q := vals.Get("q"); q != "" {
				return q, nil
			}
		}
	}
	return s, nil // raw body is the SQL
}

// mapStatus maps a ClickHouse exception code to a Tinybird-compatible HTTP
// status. ADR 0012 only requires the status *class* to be right (and the code
// itself rides X-DB-Exception-Code), so this is intentionally coarse: most CH
// exceptions are caller-fixable query errors (400); the unknown-table/database
// family is a not-found (404); a zero code means we never reached ClickHouse
// cleanly (500).
func mapStatus(code int) int {
	switch code {
	case 0:
		return http.StatusInternalServerError
	case 60, 81: // UNKNOWN_TABLE, UNKNOWN_DATABASE
		return http.StatusNotFound
	default:
		return http.StatusBadRequest
	}
}
