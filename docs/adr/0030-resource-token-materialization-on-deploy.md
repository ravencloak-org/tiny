# Resource tokens: declarative materialization on deploy, orphans warned not deleted

ADR 0005 says resource-scoped tokens are "declared in `.pipe`/`.datasource` and materialized on `tr deploy`." This pins down the flow.

- **Declaration (parity):** a `TOKEN "name" READ` line inside a `.pipe` (grants `PIPE:READ` on that pipe) or `.datasource` (`READ` / `APPEND`). The token **name** lives in git; the token **value** never does.
- **Materialize on deploy — idempotent upsert.** For each declared name, `tr deploy` looks it up in Redis (ADR 0001): absent → generate an opaque value (ADR 0005), store `value→scopes` + a `name→value` index, print the value **once** in deploy output. Present → keep the existing value. A redeploy **never rotates** a token's value — rotation would break every live client holding it.
- **Scopes are declarative — union across files.** A name declared in several files (`TOKEN "dashboard" READ` in pipe A and pipe B) is one token whose scope is the union of all its declarations, recomputed and re-applied on every deploy. Editing a file changes scope; the value is stable.
- **Value surface:** Redis holds the value; `tr token ls` and deploy output print it. Never written to git.
- **File-managed vs manual is a hard split.** Deploy reconciles **only** file-declared tokens. The bootstrap `ADMIN` token and anything from `tr token create` are manual — deploy never narrows, rotates, or prunes them.
- **Orphans are warned, not deleted.** A token whose name is declared in no file after a deploy is **orphaned**: deploy prints `token "X" no longer declared — run 'tr token rm X' to revoke` and **leaves it working**. Actual deletion is an explicit act: `tr token rm <name>` or `tr deploy --prune-tokens`.

## Considered Options

- **Auto-revoke orphans (deploy deletes the Redis key when the declaration disappears)** — rejected as the default. It is "clean" and matches ADR 0005's instant-revocation, but it makes a typo or an accidental line deletion silently kill a live integration on the next deploy. Token deletion is destructive and should be a deliberate command, not a side effect of editing a file.
- **Rotate the value on every deploy** — rejected. Breaks all existing holders of the token; defeats the purpose of a stable resource token.
- **Declarative scopes + idempotent value + warn-don't-delete orphans** — chosen.

## Consequences

- **Surprising-without-context (state in docs):** removing a `TOKEN` line and redeploying does **not** revoke the token — it only orphans it (a warning). "I deleted the declaration but the token still works" is expected; revocation is `tr token rm` / `--prune-tokens`. This is the deliberate price of not making file edits destructive.
- Instant revocation (ADR 0005) is preserved as an *intentional* operation, just not an implicit one.
- Deploy must diff declared-token-names against the file-managed set in Redis to detect orphans; manual tokens are tagged so they are never considered orphans.
- A token shared across files couples those files' scope intent; `tr token ls` should show which files declare each token so the union is auditable.
