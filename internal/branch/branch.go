// Package branch resolves the current git branch to an isolated ClickHouse
// workspace database (tr_<branch>). Per ADR 0007 branch databases are
// schema-only with an explicit lifecycle (no drop-on-merge); this package only
// computes names — creating and dropping the databases lives in internal/deploy.
package branch

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Default is the branch used when git cannot be queried — not a repo, git is
// absent, or HEAD is detached. main maps to the tr_main production database.
const Default = "main"

var (
	// runs of characters ClickHouse won't accept in an identifier → "_"
	dbDisallowed = regexp.MustCompile(`[^a-z0-9_]+`)
	// collapse any resulting "_" runs (and pre-existing ones) into a single "_"
	dbUnderscore = regexp.MustCompile(`_+`)
)

// Current returns the current git branch for dir via
// `git -C <dir> rev-parse --abbrev-ref HEAD`.
//
// It is best-effort and always returns a usable name with a nil error: when dir
// is not a git repo, git is missing, or HEAD is detached (CI often checks out a
// detached HEAD), it falls back to the TR_BRANCH env override and then to
// Default ("main"). A non-repo is a documented fallback, not a hard error, so
// callers always get a name they can hand to DBName.
func Current(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err == nil {
		// "HEAD" is what rev-parse prints for a detached HEAD — no branch name.
		if b := strings.TrimSpace(string(out)); b != "" && b != "HEAD" {
			return b, nil
		}
	}
	// ponytail: no git / detached HEAD → env override (CI sets this), else main.
	if b := strings.TrimSpace(os.Getenv("TR_BRANCH")); b != "" {
		return b, nil
	}
	return Default, nil
}

// DBName maps a git branch to its ClickHouse database name: lowercase, every
// run of non [a-z0-9_] characters replaced with "_", underscore runs collapsed,
// leading/trailing "_" trimmed, then prefixed with "tr_". The prefix also makes
// branches that start with a digit valid CH identifiers.
//
//	main               -> tr_main
//	feature/new-metric -> tr_feature_new_metric
//	Feature/X          -> tr_feature_x
func DBName(branch string) string {
	s := strings.ToLower(branch)
	s = dbDisallowed.ReplaceAllString(s, "_")
	s = dbUnderscore.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		s = Default
	}
	return "tr_" + s
}
