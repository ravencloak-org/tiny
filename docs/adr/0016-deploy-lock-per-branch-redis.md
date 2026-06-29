# tr deploy serializes per-branch via a Redis lock, fail-fast

`tr deploy` mutates ClickHouse schema and the Redis registry together; two concurrent same-branch deploys could interleave and corrupt state. We guard it with a **per-branch** Redis lock: `SET deploy:lock:tr_{branch} {deploy-id} NX EX {ttl}`. Per-branch because a branch is its own ClickHouse database (ADR 0002, 0007), so deploys to different branches never touch the same tables and run concurrently; only same-branch deploys serialize.

On contention the deploy **fails fast** — exits non-zero reporting the holder id and start time — rather than queuing. A waiting CI backlog is worse than a clean fail plus retry. Release is **owner-checked** via Lua (`if redis.call('get',k)==id then redis.call('del',k)`), so a deploy whose TTL already expired can't delete a successor's lock.

## Considered Options

- **`go-redsync` / Redlock** — rejected. Redlock solves multi-master Redis; we run a single AOF instance (ADR 0001). `SET NX` + Lua release is ~15 lines and sufficient.
- **Queue/wait on contention** — rejected. Piles up CI runs; fail-fast + retry is cleaner.
- **No lock, rely on idempotent re-apply (ADR 0006)** — rejected. Idempotency covers re-running a *completed* deploy, not two deploys interleaving mid-flight.

## Consequences

- Stale lock from a crashed deployer auto-expires after `ttl` (configurable, default ~15min); the recovery deploy is safe because apply is idempotent (ADR 0006).
- **Ceiling:** a deploy running longer than `ttl` can let a second deploy acquire while the first still runs. Mitigated by a generous TTL + idempotent apply. Upgrade path if long backfills make it real: a lock-refresh heartbeat — deliberately skipped now.
