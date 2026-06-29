package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/config"
)

// newStatusCmd builds `tr status` — shows the resolved config and pings the
// configured host's /health.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show resolved config + server reachability",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.Load()
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "host:      %s\n", cfg.Host)
			fmt.Fprintf(out, "workspace: %s\n", orDash(cfg.Workspace))
			fmt.Fprintf(out, "token:     %s\n", masked(cfg.Token))

			url := strings.TrimRight(cfg.Host, "/") + "/health"
			c := &http.Client{Timeout: 5 * time.Second}
			resp, err := c.Get(url)
			if err != nil {
				fmt.Fprintf(out, "server:    unreachable (%v)\n", err)
				return nil
			}
			defer resp.Body.Close()
			fmt.Fprintf(out, "server:    %s (%d)\n", reachWord(resp.StatusCode), resp.StatusCode)
			return nil
		},
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func masked(tok string) string {
	if tok == "" {
		return "(unset)"
	}
	if len(tok) <= 6 {
		return "set"
	}
	return tok[:3] + "…" + tok[len(tok)-2:]
}

func reachWord(status int) string {
	if status >= 200 && status < 300 {
		return "reachable"
	}
	return "responded"
}
