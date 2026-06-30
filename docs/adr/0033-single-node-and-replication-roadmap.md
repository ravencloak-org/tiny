# Single-node today; the road to replication/HA is "make `tr` cluster-aware," not "make `tr` a cluster"

TinyRaven is **single-node today** — and this is the headline production-readiness
gap, so it is recorded honestly here. ADR 0031 already fixes the scope boundary for
the `tr` *process* (it does not scale horizontally, and the reasons — in-process
gatherer buffer 0004, in-memory rate limiting 0015, single-Redis deploy lock 0016 —
are load-bearing). This ADR is the companion **roadmap for the data tier**: how
TinyRaven gets to replication/HA on OSS ClickHouse, which direction we recommend,
and what TinyRaven-side work each path implies. It is a design doc, not an
implementation plan; nothing here is built in the MVP.

## The honest current state

A default deploy is one `tr`, one ClickHouse server, one Redis. There is no
replication, no sharding, no distributed query, and no automatic failover. Losing
the ClickHouse node is downtime plus loss of any unflushed gatherer window (0004);
losing Redis loses the hot registry/cache (rebuildable from git via `tr deploy`,
which softens but does not erase the hit). The MVP is correct and simple, but it is
**not** highly available, and we do not pretend otherwise.

## Considered Options (paths to HA/replication on OSS ClickHouse)

1. **ReplicatedMergeTree + ClickHouse Keeper** — the replication primitive. Each
   table becomes `ReplicatedMergeTree`; 2–3 replicas coordinate through ClickHouse
   Keeper (built-in Raft, no ZooKeeper). Inserts to any replica propagate; reads
   serve from any replica; a node loss is survivable. This buys HA + read scaling +
   durability, **not** more single-table write/storage ceiling (it copies data, it
   does not split it). Baseline available on our 26.3 LTS floor (0009).

2. **Distributed tables + sharding** — horizontal write/storage scale. Data is
   split across N shards (each shard usually itself replicated as in option 1); a
   `Distributed` table scatter-gathers queries across shards. Solves "one node can't
   hold the data," at real operational cost: sharding-key choice, rebalancing,
   distributed-JOIN / `GLOBAL IN` caveats, insert routing.

3. **Bring-your-own ClickHouse cluster behind a stateless-ish `tr`** — TinyRaven
   stays a thin ingest/query/control proxy and the operator runs (or buys: ClickHouse
   Cloud, Altinity, Aiven) a real replicated/sharded cluster, with `tr` pointed at a
   cluster endpoint (LB / chproxy). This is the "supported scale story" already
   gestured at in 0031, made explicit.

## Recommended direction

**Option 3, leaning on option 1 as its replication primitive; option 2 deferred
(YAGNI).** TinyRaven does **not** build its own clustering. Instead we make `tr deploy`
and ENGINE generation *cluster-aware* so that an operator who points `tr` at a
replicated ClickHouse cluster gets HA data with **no `.datasource` changes** — the
same datafile that runs single-node in dev emits `ReplicatedMergeTree` against a
clustered prod target. Replication becomes a property of the *deploy target*, not of
the datafile (consistent with the branch-as-database model and the deploy-time
posture of 0009/0010). Sharding (option 2) waits until a real user outgrows a single
replicated shard. `tr`-process HA itself (multiple `tr` behind an LB) stays out of
scope per 0031 — shared rate-limit store, WAL'd ingest, and a consensus deploy lock
are its prerequisites and are already tracked there.

**Trade-offs.** Upside: minimal TinyRaven code, leans on ClickHouse's mature
replication, one datafile dev→prod, keeps the team's focus on Tinybird parity, and
lets operators use managed ClickHouse. Downside: it does **not** solve `tr`-process
HA — a single ingest/control node remains, so the unflushed gatherer window is still
at risk on a `tr` crash (0004); and the `ON CLUSTER` DDL path, Keeper macros, and
`EXCHANGE TABLES ON CLUSTER` migrations (0006) must be correct or replicas diverge.

## TinyRaven-side work each path implies

- **Registry / metadata coordination.** Redis is the metadata registry + deploy lock
  (0001/0016). HA here is the operator's concern: run HA Redis (Sentinel/Cluster,
  AOF already on); a multi-Redis deploy lock would need Redlock/`redsync`. Git stays
  the source of truth and the registry is rebuildable via `tr deploy`, so a registry
  loss is recoverable — which is why Redis HA is a *should*, not a *must*.
- **`tr deploy` against a cluster.** Emit DDL `ON CLUSTER <name>`; ENGINE templates
  select `Replicated*` engines when the target is clustered; the shadow-table →
  MV-backfill → `EXCHANGE TABLES` migration (0006) becomes its `ON CLUSTER` form,
  executed so every replica converges atomically; the `workspace_{branch}` database
  model (0007) extends to per-cluster databases or the `Replicated` database engine.
- **Connection routing.** The connection layer accepts a cluster endpoint
  (LB/chproxy) for both the native insert path (0013) and the HTTP query path; the
  gatherer flushes to a healthy replica; readiness/drain (0024) must reason about
  partial-cluster degradation rather than a single up/down.

## Tracked: two USER-blocked items (not code gaps)

Recorded here so they are not lost; both are blocked on the operator, not on
TinyRaven code:

- **Package-registry publishing (Scoop / AUR / Nix / winget).** The release steps are
  wired in `.goreleaser.yaml` (commented blocks) and documented in `docs/install.md`,
  but each is blocked on a USER-created registry plus a push secret — e.g. a
  `ravencloak-org/scoop-bucket` repo + `SCOOP_BUCKET_GITHUB_TOKEN`, an AUR package, a
  NUR repo, a `winget-pkgs` fork. Until those exist the steps stay commented/skipped.
  This is a publishing-infra gap, not a build gap.
- **Cloudflare bot-fight 403s for SDK clients.** With Cloudflare Bot Fight Mode / AI-bot
  blocking enabled, non-browser User-Agents get a 403 at the edge. Real Tinybird SDK
  clients (Python/JS/Go) send non-browser UAs, so a CF-fronted TinyRaven (e.g.
  `tiny-api.`) can block them *before* the request reaches `tr` — silently breaking
  the "drop-in: change `TINYBIRD_HOST`" promise. Fix is operator-side: a Cloudflare
  WAF / UA allowlist (or bot-fight skip rule) for the API hostname. Track as a
  required deployment caveat for any Cloudflare-fronted instance.

## Consequences

- **Surprising-without-context:** a self-hosted analytics backend that looks like it
  "just needs ClickHouse" is, by default, a single point of failure on the data tier
  too — and the chosen fix is to make `tr` cluster-*aware*, not to turn `tr` into a
  cluster. The HA story lives in ClickHouse + the operator's topology; TinyRaven's job
  is to emit the right `Replicated*` DDL `ON CLUSTER` and route to a healthy endpoint.
- Read 0033 with 0031: 0031 says the `tr` process stays single-node and why; 0033 says
  the *data* tier reaches HA via ClickHouse replication that `tr deploy` is taught to
  target. Neither is built in the MVP.
- The recommendation is deliberately the smallest change that unlocks real HA, leaving
  sharding and `tr`-process HA as later, demand-driven work.
