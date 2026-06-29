# Hot reload is dev-only; `.pipe` reloads instantly, `.datasource` routes through `tr deploy`

In dev (`tr local` / `tr dev`), a file watcher reloads project files without a restart. `.pipe` changes are pure metadata (SQL templates — no ClickHouse DDL): the file is re-parsed and **atomically swapped** into the in-memory pipe registry. `.datasource` changes imply schema DDL, so they are **not** applied instantly — they route through the **same `tr deploy` safe-migration path** (auto-apply additive, refuse breaking with a console error; ADR 0006), reusing that logic rather than forking it. The production server does **zero** file watching: its registry loads once at boot from Redis (rebuilt by `tr deploy`). Watcher = `fsnotify`, debounced ~300ms (it fires multiple events per save). A file that fails to parse leaves the old registry in place, logs to console, and never crashes the process.

## Considered Options

- **Watch and instantly apply everything, including `.datasource` → ClickHouse DDL** — rejected. A file-save silently running a schema migration bypasses the entire deploy safety model (safe-vs-breaking detection in ADR 0006, the per-branch lock in ADR 0016). The asymmetry is deliberate: queries are free to swap, DDL is not.
- **Hot reload in production too** — rejected. The production registry is rebuilt deterministically by `tr deploy` from git; a server watching files invites drift between what's deployed and what's on disk. Reload in prod = redeploy or restart.
- **Hand-rolled per-OS watcher (inotify/kqueue) or mtime polling** — polling is the zero-dep fallback but laggy and wasteful; `fsnotify` is the de-facto Go standard for a capability stdlib lacks. Chosen as a justified dependency.
- **Dev-only watcher: pipes instant, datasources via deploy** — chosen.

## Consequences

- **Surprising asymmetry (state in docs):** editing a `.pipe` updates the live endpoint instantly; editing a `.datasource` does not — the dev loop prints "run `tr deploy`" (or auto-invokes the safe path). A future reader asking "why isn't my schema change live?" is answered here.
- Reuses the deploy migration code path for `.datasource` reload — one implementation of safe/breaking logic, not two.
- `fsnotify` added to Locked Dependencies. If we ever want zero new deps, mtime polling @~1s is the drop-in fallback.
- Debounce is mandatory, not optional: editors fire several fsnotify events per save, and un-debounced reloads would thrash the registry.

## Amendment — dev `.datasource` auto-applies additive only; COW swap guarantee

Resolving the original ambiguity ("auto-invoke" vs "print run `tr deploy`"):

- **Dev `.datasource` change auto-applies *additive* migrations silently** (new column, new datasource) via the safe path (ADR 0006) — no command needed, keeping the dev loop fast. A **breaking** change (drop/retype a column, or a file delete implying a drop) is **refused** with a console message telling the developer to run `tr deploy`; the watcher never runs a destructive migration on a file save, even in dev. This is exactly ADR 0006's safe/breaking split reused at the watcher, matching `tb dev`'s live-apply-to-dev model.
- **Registry swap is copy-on-write:** both `.pipe` reload and an additive `.datasource` reload publish a new registry via an `atomic.Pointer` swap. An in-flight request completes against the snapshot it began with; new requests pick up the new registry. The read path takes no lock — handlers load the pointer once per request.
