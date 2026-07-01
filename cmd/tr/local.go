package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tinyraven/tinyraven/internal/branch"
)

func newLocalCmd() *cobra.Command {
	local := &cobra.Command{
		Use:   "local",
		Short: "Manage the local dev stack (ClickHouse + Redis + TinyRaven)",
	}
	var branchFlag string
	var assumeYes bool
	start := &cobra.Command{
		Use:   "start",
		Short: "Start the local dev stack via Docker Compose",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Preflight: without a reachable Docker daemon, `docker compose up`
			// blocks forever on the socket. Ensure one is running first.
			if err := ensureDockerDaemon(cmd.Context(), assumeYes); err != nil {
				return err
			}
			// --branch isolates data in its own ClickHouse DB tr_<branch> (ADR 0007).
			b := branchFlag
			if b == "" {
				b, _ = branch.Current(cmd.Context(), ".")
			}
			db := branch.DBName(b)
			os.Setenv("TR_CLICKHOUSE_DB", db) // compose interpolates ${TR_CLICKHOUSE_DB}
			fmt.Printf("→ branch %s -> database %s\n", b, db)
			return compose(cmd.Context(), "up", "-d")
		},
	}
	start.Flags().StringVar(&branchFlag, "branch", "",
		"isolate this branch's data in ClickHouse db tr_<branch> (default: current git branch)")
	start.Flags().BoolVarP(&assumeYes, "yes", "y", false,
		"auto-confirm installing a container runtime (for non-interactive/CI use)")
	stop := &cobra.Command{
		Use:   "stop",
		Short: "Stop the local dev stack",
		RunE: func(cmd *cobra.Command, _ []string) error {
			// No daemon means nothing is running — skip the compose call so we
			// don't hang on a dead socket.
			if !daemonUp(cmd.Context()) {
				fmt.Println("→ no Docker daemon reachable; nothing to stop")
				return nil
			}
			return compose(cmd.Context(), "down")
		},
	}
	local.AddCommand(start, stop)
	return local
}

// compose shells out to `docker compose` against the repo's docker-compose.yml.
func compose(ctx context.Context, args ...string) error {
	full := append([]string{"compose"}, args...)
	c := exec.CommandContext(ctx, "docker", full...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}

// provider is which container runtime we'll use to back the Docker daemon.
// Apple's `container` is intentionally absent: it has no `compose` verb and
// can't run this 3-service stack.
type provider int

const (
	provColima provider = iota
	provDesktop
	provInstall
)

// pickProvider chooses a runtime given what's on the machine. Pure so the
// ordering (colima before Docker Desktop, install as last resort) is testable
// without touching the real filesystem.
func pickProvider(hasColima, hasDesktop bool) provider {
	switch {
	case hasColima:
		return provColima
	case hasDesktop:
		return provDesktop
	default:
		return provInstall
	}
}

// ensureDockerDaemon guarantees a reachable Docker daemon before we run compose.
// If one is already up (from any provider, including Apple's) we use it as-is.
func ensureDockerDaemon(ctx context.Context, assumeYes bool) error {
	if daemonUp(ctx) {
		return nil
	}
	fmt.Println("→ no Docker daemon running; looking for a container runtime…")
	switch pickProvider(have("colima"), dockerDesktopInstalled()) {
	case provColima:
		return startColima(ctx)
	case provDesktop:
		return startDockerDesktop(ctx)
	default:
		return installColima(ctx, assumeYes)
	}
}

// daemonUp reports whether `docker info` succeeds. The short timeout is what
// turns a hang (dead socket) into a fast, actionable failure.
func daemonUp(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "docker", "info").Run() == nil
}

func startColima(ctx context.Context) error {
	fmt.Println("→ starting colima…")
	if err := run(ctx, "colima", "start"); err != nil {
		return fmt.Errorf("colima start failed: %w\n"+
			"if the VM won't boot (stale lima network/socket), try: colima delete -f && colima start", err)
	}
	if !daemonUp(ctx) {
		return fmt.Errorf("colima started but the Docker daemon is still unreachable")
	}
	return nil
}

func startDockerDesktop(ctx context.Context) error {
	fmt.Println("→ starting Docker Desktop…")
	if err := run(ctx, "open", "-a", "Docker"); err != nil {
		return err
	}
	return waitDaemon(ctx, 60*time.Second)
}

// installColima prompts before mutating the machine, then installs the docker
// client + colima via Homebrew and starts it.
func installColima(ctx context.Context, assumeYes bool) error {
	if !have("brew") {
		return fmt.Errorf("no container runtime found and Homebrew is missing.\n" +
			"Install one manually, e.g. https://github.com/abiosoft/colima")
	}
	if !assumeYes {
		fmt.Print("No container runtime found. Install docker + colima via Homebrew now? [y/N] ")
		if !confirm() {
			return fmt.Errorf("aborted; install a runtime, then re-run `tr local start`")
		}
	}
	if err := run(ctx, "brew", "install", "docker", "colima"); err != nil {
		return err
	}
	return startColima(ctx)
}

// waitDaemon polls until the daemon answers or the deadline passes.
func waitDaemon(ctx context.Context, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if daemonUp(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("Docker daemon did not become ready within %s", d)
}

func have(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func dockerDesktopInstalled() bool {
	_, err := os.Stat("/Applications/Docker.app")
	return err == nil
}

func run(ctx context.Context, name string, args ...string) error {
	c := exec.CommandContext(ctx, name, args...)
	c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
	return c.Run()
}

func confirm() bool {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
