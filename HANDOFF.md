# Handoff — TinyRaven design grilling (grill-with-docs)

**Date:** 2026-06-29
**Repo:** `/Users/jobinlawrance/Project/tiny` · remote `git@github.com:ravencloak-org/tiny.git` (branch `main`)
**Task:** Continue the `grill-with-docs` interview on the TinyRaven design. TinyRaven = self-hosted, drop-in Tinybird alternative in Go over OSS ClickHouse. Pre-code repo (planning docs only).

> ⚠️ **Next agent's primary ask: grill MONETIZATION.** The user explicitly queued a monetization grill for this repo. TinyRaven is Apache-2.0, self-hosted, no dashboard (API-first). Grill the business model: open-core vs managed-cloud vs support/sponsorship vs dual-license — what's the wedge, what stays free, what (if anything) is paid, and how that squares with the "free + useful software" mission (see `~/.claude/PAI/USER/TELOS/`). Capture as ADR(s) + a `docs/` note; monetization likely needs its own doc, not just an ADR. Do this **after** any technical nodes the user wants, or first if they say so.

---

## What's happened across sessions

Ran `grill-with-docs`: walk the design dependency tree, one question at a time, each with a recommended answer. Every locked decision → ADR + glossary term (if domain language) + reconciled into planning docs + propagated to GitHub issues.

**This session added a library-reuse lens** (user goal: don't reinvent the wheel, speed MVP). Result: hardest parser deleted from MVP, several deps locked to known libraries.

## Read these first (reference, do NOT re-derive)

- **Decisions:** `docs/adr/0001`–`0017` — each ADR is source of truth for one decision.
- **Glossary:** `CONTEXT.md` — canonical domain terms (now includes `pipe_stats`).
- **Design spec:** `PROMPT.md` (architecture; has a new **Locked Dependencies** table) · `MILESTONE.md` (5 phases).
- **Repo guide:** `CLAUDE.md`.
- **Backlog:** GitHub issues #1–61 under 5 phase milestones — https://github.com/ravencloak-org/tiny/issues · /milestones. Issues carry ADR refs.

## Decisions locked (full detail in each ADR)

| # | Decision | ADR |
|---|----------|-----|
| 1 | Redis-only metadata store (no Postgres); git = source of truth for defs | 0001 |
| 2 | Single-tenant: Workspace = deployment, Branch = ClickHouse DB `tr_{branch}` | 0002 |
| 3 | Pipe templating = CH parameterized queries. **MVP = value-params only (no parser); control flow → Phase 2 via `expr-lang/expr`** | 0003 (amended this session) |
| 4 | Ingestion ack-on-buffer → 202 + graceful drain; WAL later | 0004 |
| 5 | Opaque tokens in Redis (not JWT); admin bootstrap | 0005 |
| 6 | `tr deploy`: auto-apply safe migrations, refuse breaking; shadow-swap Phase 3 | 0006 |
| 7 | Branches schema-only (no data copy); explicit `tr branch rm` | 0007 |
| 8 | Datasource must be declared; reject ingestion to undefined DS | 0008 |
| 9 | Target ClickHouse 26.3 LTS feature baseline | 0009 |
| 10 | Materialized pipes: incremental MV default, refreshable when `REFRESH` declared | 0010 |
| 11 | `/v0/sql` read-only via CH `readonly=2` profile (not SQL parsing) | 0011 |
| 12 | Structural error parity (envelope + status + `X-DB-Exception-Code`) | 0012 |
| 13 | CH access split: `clickhouse-go` native for inserts, HTTP for queries | 0013 |
| 14 | `pipe_stats` recorded through the **same Gatherer** (async, best-effort, internal datasource) | 0014 ✦ |
| 15 | Rate limiting via **`go-chi/httprate`** (in-memory sliding-window, per-token); no hand-roll | 0015 ✦ |
| 16 | `tr deploy` serializes **per-branch via Redis `SET NX` lock**, fail-fast, owner-checked Lua release; no Redlock | 0016 ✦ |
| 17 | OpenAPI `/v0/openapi.json` built **at runtime from pipe registry** via `getkin/kin-openapi`; no swaggo | 0017 ✦ |

✦ = locked this session.

**Locked dependencies (PROMPT.md table):** `chi`, `go-chi/httprate`, `clickhouse-go/v2`, `redis/go-redis/v9`, `cobra`+`viper`, `prometheus/client_golang`, `expr-lang/expr` (Phase 2), `getkin/kin-openapi`, stdlib `log/slog` + `encoding/json`. Rule baked in: no new dep for what a few lines of stdlib already do.

**Other prior:** config split secret-vs-non-secret (`~/.tinyraven/config.yml` gitignored token/host, `.tr/config.yml` committed non-secret); S3/MinIO cut from scope.

---

## Remaining nodes to grill (suggested order)

**Monetization is the flagged priority (see top).** Technical nodes still open:

1. **`/v0/events` ingestion details** — NDJSON parsing, per-row validation failures, quarantine/dead-letter (Tinybird has a quarantine datasource — decide parity), max body size, `name=` required.
2. **Hot reload semantics** (Phase 1) — what reloads, atomicity, failure handling. (issue #10)
3. **Health check depth** — liveness vs readiness; which deps gate readiness. (issue #4)
4. **GitHub Actions CI/CD template** (Phase 3) — validate-on-PR / deploy-on-merge; secret injection. (issue #26)
5. **Pipe query limits / pagination** — default `LIMIT`, max rows, streaming large results, CORS for browser clients.
6. **API versioning** — `/v0` frozen for parity; how future TinyRaven-only endpoints version.
7. **Replication / multi-node HA** — likely defer; confirm + record as scope limit. (httprate-redis + CH Keeper are the upgrade paths already noted in ADRs 0015/0016.)
8. **Resource-token materialization** — exact flow of tokens declared in `.pipe`/`.datasource` → created on `tr deploy`; revocation on removal.
9. **Datasource validation** — TTL/partition/sorting-key validation at parse time; reject invalid CH engine config early. (issue #8 area)
10. **Swagger UI** — deferred from ADR 0017; needs its own issue + decision (serve where, which UI).

(Not exhaustive — also run a "what's missing" pass against `MILESTONE.md` phases 2–5.)

---

## Capture protocol (MUST follow to keep artifacts in sync)

When a decision is locked:
1. Write `docs/adr/00NN-slug.md` (next number after 0017). Format: `~/.claude/skills/grill-with-docs/ADR-FORMAT.md` — only offer an ADR when hard-to-reverse **and** surprising **and** a real trade-off.
2. Add/refine the term in `CONTEXT.md` **only if it's domain language** (not mechanism). Format in `CONTEXT-FORMAT.md`.
3. Reconcile `PROMPT.md` / `MILESTONE.md` / `README.md` / `CLAUDE.md` if they now contradict. New libs → the **Locked Dependencies** table in PROMPT.md.
4. Update the relevant GitHub issue body via `gh issue edit <n> -R ravencloak-org/tiny --body "..."` with an ADR ref; create new issues under the right phase milestone for deferred work (`gh issue create ... --milestone "Phase N — ..."`).
5. `gh` authed as `jobinlawrance`. Issues #1–61 exist; milestones = Phase 1–5. Area labels: `area:{api,cli,ingestion,query,infra,docs,connector}`.

## Active interaction modes

Caveman (terse) + Ponytail (lazy/minimal) active in the user's session. Keep prose terse; write code/docs/commits/ADRs normally. User answers grill questions tersely ("go for recommended", "parity", a pick) — treat as a decision, capture immediately, continue. User likes the library-reuse lens — keep applying it (reuse before reinvent, stdlib before dep).

## Suggested skills for the next agent

- **`grill-with-docs`** — resume the interview (core task). Base dir `~/.claude/skills/grill-with-docs`.
- **`context7`** (MCP) — pull current ClickHouse / `clickhouse-go` / lib docs when a node needs exact API/feature detail (used heavily this session for httprate/kin-openapi/expr).
- **WebSearch / `last30days`** — verify Tinybird behavior parity + research OSS monetization models for the monetization grill.
