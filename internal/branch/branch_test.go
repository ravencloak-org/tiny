package branch

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestDBName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"main", "tr_main"},
		{"feature/new-metric", "tr_feature_new_metric"},
		{"Feature/X", "tr_feature_x"},         // lowercased
		{"FixBug", "tr_fixbug"},               // uppercase only
		{"release-1.2.3", "tr_release_1_2_3"}, // dashes + dots
		{"feature/JIRA-42_thing", "tr_feature_jira_42_thing"},
		{"2-hotfix", "tr_2_hotfix"},              // leading digit kept; tr_ prefix keeps it valid
		{"a//b--c", "tr_a_b_c"},                  // runs collapse to single _
		{"feature__double", "tr_feature_double"}, // pre-existing underscore run collapsed
		{"/leading/", "tr_leading"},              // leading/trailing separators trimmed
		{"___", "tr_main"},                       // empty after sanitize → Default
		{"", "tr_main"},                          // empty input → Default
		{"用户/metric", "tr_metric"},               // non-ASCII dropped, rest kept
	}
	for _, c := range cases {
		if got := DBName(c.in); got != c.want {
			t.Errorf("DBName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCurrent_GitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	// Build a throwaway repo so the test does not depend on the surrounding tree.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "feature/new-metric")
	run("config", "user.email", "t@t.test")
	run("config", "user.name", "t")
	if err := os.WriteFile(dir+"/f", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "f")
	run("commit", "-q", "-m", "init")

	got, err := Current(context.Background(), dir)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got != "feature/new-metric" {
		t.Errorf("Current = %q, want feature/new-metric", got)
	}
}

func TestCurrent_NonRepoFallsBackToDefault(t *testing.T) {
	// TR_BRANCH must be unset for the Default path to win.
	t.Setenv("TR_BRANCH", "")
	dir := t.TempDir() // plain dir, not a git repo
	got, err := Current(context.Background(), dir)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got != Default {
		t.Errorf("Current = %q, want %q", got, Default)
	}
}

func TestCurrent_EnvOverrideWhenNoRepo(t *testing.T) {
	t.Setenv("TR_BRANCH", "ci-branch")
	dir := t.TempDir()
	got, err := Current(context.Background(), dir)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if got != "ci-branch" {
		t.Errorf("Current = %q, want ci-branch", got)
	}
}
