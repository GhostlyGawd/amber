# Privacy & security posture

The absence of growth-hacking is the growth hack. No telemetry, no nags,
no account, no key.

## Zero telemetry by default

Amber ships with telemetry **off**. `init` asks one question — whether to
keep **local-only** usage counters (command counts, never content) — and
the default is **No**. v1 has no upload endpoint at all: even if you
enable counters, there is nowhere for them to go. They live in the
`meta` table and are visible in `amber status --format json`.

If a future version ever adds an endpoint, the payload will be printed
before any send, and the default will remain No.

## Secret + PII scanning

Runs on **write** and on **export** (§10). Patterns plus entropy detect:

- **Secrets**: AWS keys, GitHub/GitLab tokens, Slack tokens, OpenAI /
  Anthropic keys, Stripe keys, Google API keys, npm tokens, private-key
  blocks, JWTs, and high-entropy strings near credential words.
- **PII**: emails, US SSNs, phone numbers, credit-card numbers
  (Luhn-checked), IBANs.

Policy is configurable (`scan.mode`):

- `warn` (default): a finding refuses the write unless `--force`. With
  `--force`, **secrets are stored redacted** (`[redacted:aws-access-key]`)
  — a leaked key is never a useful memory — and PII is stored as given
  (you explicitly confirmed) with a warning.
- `block`: findings always refuse, even with `--force`.

`digest` drops flagged candidates outright. `export` scans outgoing
content, redacts secrets, prints a summary, and still warns you to review
before sharing (Codex parity: redact *and* warn).

## File permissions

The database and config are `0600`. Project stores (`./.amber/`) ship a
`.gitignore` covering the DB, its WAL, and its SHM — the database never
lands in version control. Exports (plain text you reviewed) are what you
commit.

## Portability and deletion semantics

- **Reversible soft deletion**: `amber forget --entity "Alice Chen"`
  tombstones every linked memory. Tombstoned content remains in SQLite,
  including the operation journal, until a future physical-purge feature is
  implemented. Do not treat `forget` as regulatory erasure or secure media
  deletion.
- **Portability**: `amber export` produces the open amber.v1 interchange
  format. Import validates the full stream and restores IDs, timestamps,
  supersedence, entity aliases, and tags in one transaction.

## What leaves your machine

In the default configuration: **nothing**, after the one-time optional
model download at `init`. The only network operations Amber can perform
are:

1. fetching the local embedding model once at `init` (offline forever
   after);
2. calling your configured digest LLM (`claude -p` locally, or an API you
   opted into) during `digest`;
3. calling an embeddings endpoint **only** if you explicitly chose the
   `openai-compat` provider. Quarantined and tombstoned content is not sent for
   embedding. Review approval can make a record active and eligible for
   embedding.

API keys are referenced by environment-variable name in config, never
stored in the config file or the database.
