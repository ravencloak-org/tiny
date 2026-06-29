# Handoff — TinyRaven design grilling (grill-with-docs)

**Date:** 2026-06-29
**Repo:** `/Users/jobinlawrance/Project/tiny` · remote `git@github.com:ravencloak-org/tiny.git` (branch `main`, pushed through commit `77768fd`)
**Task:** Continue the `grill-with-docs` interview on the TinyRaven design. TinyRaven = self-hosted, drop-in Tinybird alternative in Go over OSS ClickHouse. Pre-code repo (planning docs only).

---

## What this session did

Ran the `grill-with-docs` skill: walked the design dependency tree, one question at a time, each with a recommended answer. Every resolved decision was captured as an ADR + glossary term + reconciled into the planning docs + propagated to GitHub issues. **13 decisions locked.**

## Read these first (do NOT re-derive — reference, don't duplicate)

- **Decisions:** `docs/adr/0001`–`0013` — each ADR is the source of truth for one decision (rationale + trade-offs + consequences).
- **Glossary:** `CONTEXT.md` — canonical domain terms (Datasource, Pipe, Gatherer, Workspace, Branch, Token, Template, Materialized pipe, Metadata Registry, Source of truth).
- **Design spec:** `PROMPT.md` (architecture, reconciled with all ADRs) · `MILESTONE.md` (5 phases).
- **Repo guide:** `CLAUDE.md`.
- **Backlog:** GitHub issues #1–60 under 5 phase milestones — https://github.com/ravencloak-org/tiny/issues and /milestones. Issues carry ADR refs in their bodies.

## Decisions locked (one line each — full detail in the ADR)

| # | Decision | ADR |
|---|----------|-----|
| 1 | Redis-only metadata store (no Postgres); git = source of truth for defs | 0001 |
| 2 | Single-tenant: Workspace = deployment, Branch = ClickHouse DB `tr_{branch}` | 0002 |
| 3 | Pipe templating = ClickHouse parameterized queries (no `text/template`); MVP = common subset | 0003 |
| 4 | Ingestion ack-on-buffer → 202 + graceful drain; WAL later; not async_insert | 0004 |
| 5 | Opaque tokens in Redis (not JWT); admin token bootstrap; no TTL on static tokens | 0005 |
| 6 | `tr deploy`: auto-apply safe migrations, refuse breaking; shadow-swap Phase 3 | 0006 |
| 7 | Branches schema-only (no data copy); explicit `tr branch rm` (no drop-on-merge) | 0007 |
| 8 | Datasource must be declared; reject ingestion to undefined DS; default `MergeTree ORDER BY tuple()` | 0008 |
| 9 | Target ClickHouse 26.3 LTS; build on query_cache / refreshable MV / native JSON; cut Redis query-cache | 0009 |
| 10 | Materialized pipes: incremental MV (backfill) default, refreshable MV when `REFRESH` declared | 0010 |
| 11 | `/v0/sql` read-only enforced by CH `readonly=2` profile + caps (not SQL parsing); separate RW/RO CH users | 0011 |
| 12 | Structural error parity (envelope + status + `X-DB-Exception-Code`), not message text | 0012 |
| 13 | ClickHouse access split: `clickhouse-go` native driver for inserts, HTTP interface for queries | 0013 |

Other: CLI = `cobra` + `viper` (OpenTUI rejected — TS + TUI shape); config split secret-vs-non-secret (`~/.tinyraven/config.yml` = token/host gitignored, `.tr/config.yml` = committed non-secret); S3/MinIO cut from scope.

---

## OPEN — in progress

**Q15 (not yet answered): pipe_stats observability recording.** The trap: synchronous INSERT-per-query adds query latency + floods ClickHouse with tiny parts. **My recommendation:** record `tinybird.pipe_stats` as an *internal datasource fed through the same Gatherer* (async, post-response, batched) — reuse the ingestion path, no per-query insert. Alternative: a dedicated stats writer. **Ask the user to pick, then capture (ADR 0014 + update issue, Phase 2).**

## Remaining nodes to grill (suggested order)

Roughly Phase-2/3 leaning; pick by dependency. Each should get the same treatment (one Q, recommend, capture).

1. **pipe_stats recording** (Q15 above) — finish this first.
2. **Rate-limiting algorithm** — fixed-window vs sliding-window/token-bucket (Redis); per-token+per-pipe; `429` + `Retry-After`/rate headers. (Phase 2, issue #17)
3. **Deploy concurrency / locking** — concurrent `tr deploy` safety; a Redis deploy-lock? idempotent re-apply already chosen (ADR 0001/0006) — nail the lock.
4. **OpenAPI generation** — how the spec is generated from the pipe registry; served where. (issue #11/#14)
5. **`/v0/events` ingestion details** — NDJSON parsing, per-row validation failures, quarantine/dead-letter behavior, max body size, `name=` required. (Tinybird has a quarantine datasource — decide parity.)
6. **Hot reload semantics** (Phase 1) — what reloads, atomicity, failure handling. (issue #10)
7. **Health check depth** — liveness vs readiness; which deps gate readiness. (issue #4)
8. **GitHub Actions CI/CD template** (Phase 3) — validate-on-PR / deploy-on-merge; how secrets are injected. (issue #21 area)
9. **Pipe query limits / pagination** — default `LIMIT`, max rows, streaming large results, CORS for browser clients.
10. **API versioning** — `/v0` frozen for Tinybird parity; how future TinyRaven-only endpoints version.
11. **Replication / multi-node** — is HA in scope? ClickHouse Keeper/replicas; Redis HA. (likely defer — confirm + record as scope limit.)
12. **Resource-token materialization** — exact flow of tokens declared in `.pipe`/`.datasource` → created on `tr deploy`; revocation on removal.
13. **Datasource validation** — TTL/partition/sorting-key validation at parse time; rejecting invalid CH engine config early.

(Not exhaustive — the next agent should also run a "what's missing" pass against `MILESTONE.md` phases 2–5.)

---

## Capture protocol (MUST follow to keep artifacts in sync)

When a decision is locked:
1. Write `docs/adr/00NN-slug.md` (next number after 0013). Format: `/Users/jobinlawrance/.claude/skills/grill-with-docs/ADR-FORMAT.md` — only offer an ADR when hard-to-reverse **and** surprising **and** a real trade-off.
2. Add/refine the term in `CONTEXT.md` (glossary only, no implementation; format in the skill's `CONTEXT-FORMAT.md`).
3. Reconcile `PROMPT.md` / `MILESTONE.md` / `README.md` / `CLAUDE.md` if they now contradict.
4. Update the relevant GitHub issue body via `gh issue edit <n> -R ravencloak-org/tiny --body "..."` with an ADR ref; create new issues under the right phase milestone for deferred work.
5. `gh` is authed as `jobinlawrance`. Issues #1–60 exist; milestones = Phase 1–5. Area labels: `area:{api,cli,ingestion,query,infra,docs,connector}`.

## Environment note (resolved this session)

`~/.claude/settings.json` had two PAI hooks pinned to a deleted node path (`Cellar/node/26.0.0`); fixed to `/opt/homebrew/bin/node`. Fully resolved — mentioned only so the next agent isn't surprised by the earlier error.

## Active interaction modes

Caveman (terse) + Ponytail (lazy/minimal) skills are active in the user's session. Keep prose terse; write code/docs/commits normally. The user answers grill questions tersely ("works", "parity", a pick) — treat as decisions, capture immediately, continue.

---

## Suggested skills for the next agent

- **`grill-with-docs`** — resume the interview (this is the core task). Base dir `/Users/jobinlawrance/.claude/skills/grill-with-docs`.
- **`code-review` / `simplify`** — later, once code exists (none yet).
- **`context7`** (MCP) — pull current ClickHouse / `clickhouse-go` / cobra docs when a grill node needs exact API/feature detail.
- **WebSearch** — for verifying Tinybird behavior parity (used this session for metadata model, error format, CH versions).
