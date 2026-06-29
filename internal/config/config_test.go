package config

import (
	"os"
	"path/filepath"
	"testing"
)

// clearClientEnv unsets every env var that influences Host/Token/Workspace so a
// stray value in the developer's/CI shell can't taint a precedence assertion.
func clearClientEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"TINYBIRD_HOST", "TINYBIRD_TOKEN", "TINYBIRD_WORKSPACE",
		"TR_HOST", "TR_TOKEN", "TR_WORKSPACE",
	} {
		t.Setenv(k, "")
	}
}

// writeHome points HOME at a temp dir and writes ~/.tinyraven/config.yml.
func writeHome(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".tinyraven")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// chdirProject switches CWD to a temp dir, optionally writing .tr/config.yml.
func chdirProject(t *testing.T, body string) {
	t.Helper()
	proj := t.TempDir()
	t.Chdir(proj)
	if body == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(proj, ".tr"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".tr", "config.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_DefaultsWhenNothingSet(t *testing.T) {
	clearClientEnv(t)
	t.Setenv("HOME", t.TempDir()) // no ~/.tinyraven/config.yml
	chdirProject(t, "")           // no .tr/config.yml

	c := Load()
	if c.Host != "http://localhost:8000" {
		t.Errorf("Host = %q, want default http://localhost:8000", c.Host)
	}
	if c.Token != "" || c.Workspace != "" {
		t.Errorf("Token/Workspace = %q/%q, want empty", c.Token, c.Workspace)
	}
	// Existing server defaults must keep working.
	if c.HTTPAddr != ":8000" || c.CHDatabase != "tr_main" {
		t.Errorf("server defaults changed: HTTPAddr=%q CHDatabase=%q", c.HTTPAddr, c.CHDatabase)
	}
}

func TestLoad_HomeFileBeatsDefault(t *testing.T) {
	clearClientEnv(t)
	writeHome(t, "host: https://home.example\ntoken: home-tok\nworkspace: home-ws\n")
	chdirProject(t, "")

	c := Load()
	if c.Host != "https://home.example" || c.Token != "home-tok" || c.Workspace != "home-ws" {
		t.Errorf("home file not applied: %+v", c)
	}
}

func TestLoad_ProjectFileBeatsHome(t *testing.T) {
	clearClientEnv(t)
	writeHome(t, "host: https://home.example\ntoken: home-tok\nworkspace: home-ws\n")
	// Project overrides host + workspace but not token (token stays from home).
	chdirProject(t, "host: https://project.example\nworkspace: project-ws\n")

	c := Load()
	if c.Host != "https://project.example" {
		t.Errorf("Host = %q, want project value", c.Host)
	}
	if c.Workspace != "project-ws" {
		t.Errorf("Workspace = %q, want project-ws", c.Workspace)
	}
	if c.Token != "home-tok" {
		t.Errorf("Token = %q, want home-tok (project file omits it)", c.Token)
	}
}

func TestLoad_EnvBeatsFiles(t *testing.T) {
	clearClientEnv(t)
	writeHome(t, "host: https://home.example\ntoken: home-tok\n")
	chdirProject(t, "host: https://project.example\n")
	t.Setenv("TINYBIRD_HOST", "https://env.example")
	t.Setenv("TINYBIRD_TOKEN", "env-tok")
	t.Setenv("TINYBIRD_WORKSPACE", "env-ws")

	c := Load()
	if c.Host != "https://env.example" || c.Token != "env-tok" || c.Workspace != "env-ws" {
		t.Errorf("env did not win: %+v", c)
	}
}

func TestLoad_TRAliasEnv(t *testing.T) {
	clearClientEnv(t)
	t.Setenv("HOME", t.TempDir())
	chdirProject(t, "")
	t.Setenv("TR_HOST", "https://tr-alias.example")

	if c := Load(); c.Host != "https://tr-alias.example" {
		t.Errorf("Host = %q, want TR_HOST alias value", c.Host)
	}
}

func TestLoad_TinybirdBeatsTRAlias(t *testing.T) {
	clearClientEnv(t)
	t.Setenv("HOME", t.TempDir())
	chdirProject(t, "")
	t.Setenv("TR_HOST", "https://tr-alias.example")
	t.Setenv("TINYBIRD_HOST", "https://tinybird.example")

	if c := Load(); c.Host != "https://tinybird.example" {
		t.Errorf("Host = %q, want TINYBIRD_HOST to win over TR_HOST", c.Host)
	}
}

func TestLoadConfigFile_TildeExpansion(t *testing.T) {
	writeHome(t, "host: https://tilde.example\ntoken: t\n")
	fc, err := LoadConfigFile("~/.tinyraven/config.yml")
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if fc.Host != "https://tilde.example" || fc.Token != "t" {
		t.Errorf("parsed = %+v, want host/token from file", fc)
	}
}

func TestLoadConfigFile_MissingReturnsNotExist(t *testing.T) {
	_, err := LoadConfigFile(filepath.Join(t.TempDir(), "nope.yml"))
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want os.IsNotExist", err)
	}
}

func TestWriteConfigFile_RoundTripAndPerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yml")
	in := FileConfig{Host: "https://w.example", Token: "secret", Workspace: "ws"}
	if err := WriteConfigFile(path, in); err != nil {
		t.Fatalf("WriteConfigFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600 (holds token)", perm)
	}

	out, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if out != in {
		t.Errorf("round-trip = %+v, want %+v", out, in)
	}
}
