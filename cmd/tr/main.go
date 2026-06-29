// Command tr is the TinyRaven binary: both the HTTP server and the CLI.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "tr",
		Short:         "TinyRaven — self-hosted, Tinybird-compatible analytics backend",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}
	root.AddCommand(newServeCmd(), newLocalCmd(), newDeployCmd(), newTokenCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
