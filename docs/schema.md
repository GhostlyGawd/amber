# Storage schema

One SQLite file (`amber.db`), WAL mode, `0600`. Pure-Go driver
(`modernc.org/sqlite`) — no CGO, no extension. You can open it with any
`sqlite3`; nothing is hidden from you.

## Tables

### `memories`
The belief records.

| Column | Notes |
|---|---|
| `id` | 128-bit random, Crockford base32, lowercase (26 chars) |
| `content` | the claim (≤4000 chars — memories are claims, not documents) |
| `type` | `fact` \| `preference` \| `decision` \| `event` \| `note` |
| `importance` | 1–5 |
| `trust` | 0=user-stated, 1=user-approved, 2=auto-digest, 3=untrusted-origin |
| `confidence` | 0–1; decays per type (aging) |
| `last_confirmed_at` | anchor for recency decay and passive reconfirmation |
| `scope` | `global` \| `project` |
| `source`, `session_id` | provenance |
| `created_at`, `updated_at` | RFC3339 UTC |
| `status` | `active` \| `superseded` \| `tombstoned` \| `quarantined` \| `aging` |
| `superseded_by` | id of the memory that replaced this one |
| `embedding` | little-endian float32 BLOB (or NULL on the BM25 floor) |
| `content_hash` | sha256 of normalized content; powers dedupe |

Statuses are never hard-deleted. Every transition is a soft update
journaled to `ops`.

### `entities` + `memory_entities`
People, projects, orgs — with `aliases_json` and a many-to-many link.

### `tags` + `memory_tags`
Free-form labels.

### `memories_fts`
FTS5 virtual table (contentless, `unicode61`) over `content`, keyed to
the `memories` rowid. Powers lexical/BM25 recall.

### `ops` — append-only journal (decision D14)
`(id, ts, op, memory_id, payload)`. Never updated, never deleted. Every
mutation records enough JSON payload to reverse it. This is what powers
`amber restore`, and what makes future sync a merge problem rather than a
rewrite. It also defines the *vector epoch* — `MAX(id)` over
vector-relevant ops — used to invalidate the search cache.

### `flags`
Review-inbox markers beyond quarantine: `needs-review` (auto-digested
T2), `ambiguity` (unresolved contradiction kept as both),
`instruction-shape`, `tainted`. Additive to the spec's §6 schema.

### `meta`
`schema_version`, `embedding_model` (identity pinned on first embedded
write — mixed-model stores are refused at open), `embedding_dims`,
`store_created_at`, local usage counters (`counter.*`).

## Concurrency

`BEGIN IMMEDIATE` (via `_txlock=immediate`) wraps belief adjudication and its
write in one transaction. The same transaction includes insert or reconfirm,
supersedence, flags, operation-journal entries, and first-write embedding-model
pinning. Multiple agents or panes therefore cannot decide against the same
stale candidate snapshot. WAL and `busy_timeout=5000` handle contention.

`amber doctor --reembed` computes replacement vectors only for active and
aging memories. It then replaces those vectors, clears non-searchable vectors,
pins the new model and dimensions, and journals the migration in one
transaction. JSONL import also validates the complete input before one
transaction restores records, timestamps, supersedence, entity aliases, and
tags.

## Sidecar

`vectors.cache` next to the DB is a disposable, memoized, epoch-keyed
copy of active/aging embeddings for fast brute-force cosine. Deleting it
is always safe; it rebuilds on next recall.
