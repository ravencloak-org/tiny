# MVP is single-node: the `tr` process does not scale horizontally (backing stores may)

TinyRaven's MVP runs as **one `tr` process**. ClickHouse and Redis behind it may be clustered on their own terms, but the TinyRaven layer itself is a single node — running multiple `tr` instances behind a load balancer is **explicitly not supported** in the MVP. This is a deliberate scope boundary, recorded in one place because several already-locked decisions quietly assume it.

## Why the single-node assumption is load-bearing

- **Gatherer buffers in-process (ADR 0004).** Events sit in a Go channel/buffer until flush; a node crash loses the unflushed window. Two nodes don't fix this — they double the exposure, and split a logical ingest stream across two independent buffers.
- **Rate limiting is in-memory per-node (ADR 0015).** N nodes means N× the configured limit, because each holds its own counter. A per-token limit stops being a global limit.
- **Deploy lock is single-Redis `SET NX` (ADR 0016).** Correct for one deployer against one Redis; it is not a multi-Redis consensus lock.

Because ingestion and rate-limiting are node-local, even the "queries are stateless, just put `tr` behind an LB" half-measure is unsafe — the same fleet that scales reads breaks ingest accounting. So we do not claim partial HA.

## What scaling *is* available in the MVP

- **Vertical:** one larger `tr` node (it is mostly an I/O proxy + batcher; it scales up well).
- **Backing-store clustering, independently:** a single `tr` in front of a **ClickHouse cluster** (ReplicatedMergeTree + CH Keeper) and an HA Redis (Sentinel/Cluster, AOF already on) is the supported "scale" story. The data tier scales; the `tr` control/ingest tier does not, yet.

## Considered Options

- **Design for horizontal `tr` HA now** (shared rate-limit store, WAL'd ingest, consensus deploy lock, leader election) — rejected for MVP. It is a large amount of distributed-systems work for a self-hosted tool whose first job is single-node parity with Tinybird's developer experience. YAGNI until someone runs at a scale one node can't hold.
- **Single-node MVP with documented upgrade paths** — chosen.

## Consequences / upgrade paths (documented, not built)

- Rate limiting → `httprate-redis` (shared counter across nodes) — already the noted path in ADR 0015.
- Deploy lock → Redlock/`redsync` across multiple Redis, or keep one HA Redis — noted in ADR 0016.
- Gatherer durability → a write-ahead log before flush — already the "WAL later" path in ADR 0004.
- ClickHouse / Redis HA are the operator's concern via each system's native clustering, not TinyRaven code.
- **Surprising-without-context:** a server you might expect to scale out behind an LB does not, by design, in the MVP. This ADR is the answer to "can I run three `tr` nodes?" — not yet, run one bigger one in front of clustered stores.
