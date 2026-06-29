// Package buildanchor pins Phase 2 dependencies in go.mod/go.sum before the
// parallel package work starts, so concurrent agents never race on go.mod.
// Deleted at integration once real imports exist.
package buildanchor

import (
	_ "github.com/go-chi/httprate"
	_ "github.com/prometheus/client_golang/prometheus"
)
