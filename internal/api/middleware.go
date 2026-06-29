package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/tinyraven/tinyraven/internal/model"
)

type ctxKey int

const tokenCtxKey ctxKey = iota

// authMiddleware validates the bearer token (or ?token= query param for browser
// embedding, ADR 0025) against the TokenStore and stashes it in the context.
func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := bearerToken(r)
		if value == "" {
			writeError(w, http.StatusUnauthorized, "missing authentication token")
			return
		}
		tok, ok, err := s.deps.Tokens.Validate(r.Context(), value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "token validation failed")
			return
		}
		if !ok {
			writeError(w, http.StatusForbidden, "invalid authentication token")
			return
		}
		ctx := context.WithValue(r.Context(), tokenCtxKey, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken pulls the token from the Authorization header, falling back to the
// ?token= query param (resource-scoped browser use, ADR 0025).
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if v, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// tokenFrom returns the authenticated token from the request context, if any.
func tokenFrom(ctx context.Context) (*model.Token, bool) {
	t, ok := ctx.Value(tokenCtxKey).(*model.Token)
	return t, ok
}

// allow reports whether the token may perform verb on resource (ADR 0005).
// model.Token.HasScope already treats ADMIN as a superuser; "<verb>:*" grants
// the verb on every resource (e.g. READ:* = read any pipe).
func allow(tok *model.Token, verb, resource string) bool {
	return tok != nil && (tok.HasScope(verb+":"+resource) || tok.HasScope(verb+":*"))
}

// adminOnly gates a route to tokens carrying the ADMIN scope (e.g. /v0/sql,
// which can read arbitrary SQL). Runs after authMiddleware, so the token is in
// the context.
func (s *server) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, _ := tokenFrom(r.Context())
		if tok == nil || !tok.HasScope("ADMIN") {
			writeError(w, http.StatusForbidden, "this endpoint requires the ADMIN scope")
			return
		}
		next.ServeHTTP(w, r)
	})
}
