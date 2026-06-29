# TinyRaven CI/CD templates

Drop-in GitHub Actions workflow for deploying a TinyRaven data project
(`.datasource` / `.pipe` files) the same way you would with Tinybird.

## Usage

1. Copy the workflow into your repo:

   ```bash
   mkdir -p .github/workflows
   cp path/to/tinyraven/templates/ci/github-actions.yml .github/workflows/tinyraven.yml
   ```

2. Add two repository secrets (**Settings → Secrets and variables → Actions**):

   | Secret            | Value                                              |
   |-------------------|----------------------------------------------------|
   | `TINYBIRD_HOST`   | Your TinyRaven API host, e.g. `https://tr.example` |
   | `TINYBIRD_TOKEN`  | An admin/deploy token                              |

   The `tr` CLI reads these from the environment, and env overrides any
   `~/.tinyraven/config.yml` / `.tr/config.yml` (precedence is set in
   `internal/config`).

3. Edit the two `Install tr` steps to match how you ship the binary
   (Phase 4 adds `brew` / `apt`; until then `go install
   github.com/tinyraven/tinyraven/cmd/tr@latest` works).

## What it does

| Event                | Job        | Command                              | Effect                                            |
|----------------------|------------|--------------------------------------|---------------------------------------------------|
| Pull request → main  | `validate` | `tr deploy --check --project-dir .`  | Parses + diffs schema, fails on breaking changes; **applies nothing**. |
| Push to main (merge) | `deploy`   | `tr deploy --project-dir .`          | Applies safe migrations to the `tr_main` workspace. |

Per [ADR 0007](../../docs/adr/0007-branch-schema-only-explicit-lifecycle.md),
branch databases (`tr_<branch>`) are schema-only and isolated, so validating a
PR never reads or mutates production data.

## Notes

- **No `--check` in your `tr`?** It's the intended dry-run flag. If your
  installed binary predates it, validate against a throwaway branch DB instead:
  check out the PR branch (so `tr` resolves `tr_<branch>`), run
  `tr deploy --project-dir .`, then `tr branch rm <branch>`.
- **Breaking changes** (e.g. dropping/retyping a column) are refused unless you
  pass `--allow-breaking`; keep that out of the automated `deploy` job and run
  it deliberately from a human-triggered workflow.
- The `concurrency` block serializes deploys so two merges can't race on the
  same workspace.
