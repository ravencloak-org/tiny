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
