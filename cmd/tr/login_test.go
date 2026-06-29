package main

import (
	"io"
	"testing"

	"github.com/tinyraven/tinyraven/internal/config"
)

// TestLoginWritesConfig runs `tr login --host --token --workspace` against a
// temp HOME and verifies the config file round-trips.
func TestLoginWritesConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newLoginCmd()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"--host", "http://example:8000", "--token", "tok-123", "--workspace", "ws1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("login: %v", err)
	}

	fc, err := config.LoadConfigFile(config.HomeConfigPath)
	if err != nil {
		t.Fatalf("read back config: %v", err)
	}
	if fc.Host != "http://example:8000" || fc.Token != "tok-123" || fc.Workspace != "ws1" {
		t.Fatalf("config mismatch: %+v", fc)
	}
}
