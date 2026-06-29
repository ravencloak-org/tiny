package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/config"
)

// newLoginCmd builds `tr login` — saves host + token to ~/.tinyraven/config.yml
// (Tinybird-compatible; ADR 0032). The file is written 0600.
func newLoginCmd() *cobra.Command {
	var host, token, workspace string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save API host + token to ~/.tinyraven/config.yml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cur := config.Load()
			if host == "" {
				host = cur.Host
			}
			if workspace == "" {
				workspace = cur.Workspace
			}
			if token == "" {
				fmt.Fprint(cmd.OutOrStdout(), "Token: ")
				line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
				token = strings.TrimSpace(line)
			}
			if token == "" {
				return fmt.Errorf("a token is required")
			}
			if err := config.WriteConfigFile(config.HomeConfigPath, config.FileConfig{
				Host: host, Token: token, Workspace: workspace,
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ saved %s (host %s)\n", config.HomeConfigPath, host)
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "API host (default: current config/env)")
	cmd.Flags().StringVar(&token, "token", "", "API token (prompted if omitted)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace name (optional)")
	return cmd
}
