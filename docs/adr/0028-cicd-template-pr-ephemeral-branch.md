# CI/CD template: PR validates on a live ephemeral branch, merge deploys to prod

The shipped GitHub Actions template (for a user's `.datasource`/`.pipe` project repo) is **two jobs**, deliberately split by trust and purpose:

**PR job — validate on a live throwaway branch (no prod secrets).** Spins `clickhouse/clickhouse-server:26.3` + Redis as GitHub Actions **service containers**, then `tr deploy`s the project into an ephemeral branch database `tr_pr_{number}` (ADR 0002/0007). Because the deploy actually runs the `CREATE TABLE`s, it catches the type/engine-param errors that ADR 0027 delegates to ClickHouse — *before* merge, not after. Static `--dry-run` validation alone would pass a bad column type and let the merge deploy fail on a green PR. A pipe-test step (`tr test`, if/when that command exists) slots in here later; not assumed now.

**Merge job — deploy to production.** On push to `main`, `tr deploy` against the real host, with `TINYBIRD_HOST` + `TINYBIRD_TOKEN` from GitHub repository secrets, never echoed.

**Teardown.** A `pull_request: closed` trigger runs `tr branch rm tr_pr_{number}` (explicit lifecycle, ADR 0007). The ephemeral branch never lingers.

**Key security property:** the PR job needs **no production secrets** — it validates against the in-CI service-container ClickHouse, not the real backend. So PRs from forks validate normally (forked PRs can't read repo secrets), and only the merge-to-main deploy — which runs from trusted `main` — touches prod credentials.

## Target workflow (Phase 3 — the canonical spec; YAML committed when `tr deploy` exists)

```yaml
on:
  pull_request: { types: [opened, synchronize, reopened, closed] }
  push: { branches: [main] }

jobs:
  validate:                       # PR only, no prod secrets
    if: github.event_name == 'pull_request' && github.event.action != 'closed'
    runs-on: ubuntu-latest
    services:
      clickhouse: { image: clickhouse/clickhouse-server:26.3, ports: ['8123:8123','9000:9000'] }
      redis:      { image: redis:7, ports: ['6379:6379'] }
    steps:
      - uses: actions/checkout@v4
      - run: tr deploy --branch "tr_pr_${{ github.event.number }}"   # real CREATE TABLE -> catches CH-semantic errors

  teardown:                       # PR closed
    if: github.event_name == 'pull_request' && github.event.action == 'closed'
    runs-on: ubuntu-latest
    services: { clickhouse: {...}, redis: {...} }
    steps:
      - run: tr branch rm "tr_pr_${{ github.event.number }}"

  deploy:                         # merge to main, prod secrets
    if: github.event_name == 'push'
    runs-on: ubuntu-latest
    env:
      TINYBIRD_HOST:  ${{ secrets.TINYBIRD_HOST }}
      TINYBIRD_TOKEN: ${{ secrets.TINYBIRD_TOKEN }}
    steps:
      - uses: actions/checkout@v4
      - run: tr deploy
```

## Considered Options

- **Static `--dry-run` only (no ClickHouse in CI)** — rejected: cannot catch the CH-semantic errors ADR 0027 puts on ClickHouse, so a green PR can still break `main` on deploy. Offered only as a lighter documented variant.
- **Validate against a shared staging backend with prod secrets in PR** — rejected: leaks prod credentials to PR jobs (incl. forks) and lets concurrent PRs collide. Ephemeral in-CI branch is isolated and secret-free.

## Consequences

- The PR job's branch deploy depends on `tr deploy --branch` accepting an explicit branch override rather than only git-branch detection (ADR 0002 detects the *git* branch; CI needs to name `tr_pr_{number}`). That flag must exist for this template.
- ~20–30 s CI overhead per PR for CH/Redis spin-up — accepted for real validation.
- Template ships as documented example + optional `tr init` scaffolding; the runnable YAML lands in Phase 3 alongside `tr deploy`.

## References

- Builds on ADR 0002 (branch = DB), 0006 (safe/breaking deploy), 0007 (explicit branch lifecycle), 0016 (deploy lock), 0027 (validation boundary).
- Issue #26 (Phase 3).
