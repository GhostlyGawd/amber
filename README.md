# Amber

**Local-first, long-term memory for AI coding agents.** One static Go
binary, one SQLite file you own. No Docker, no API key, no account,
offline by default.

> Instructions are what you *tell* an agent (CLAUDE.md / AGENTS.md).
> Memory is what it *learns* (Amber).

*Memory you can read.* Every memory is inspectable, every write is
previewable as a diff, every change is reversible, every untrusted input
is quarantined.

> **Name notice.** "Amber" is provisional — it collides with
> amber-lang.com and others. Paths, env vars, and the binary are all
> `amber` / `~/.amber/` / `AMBER_*` so a rename is a mechanical
> find/replace. See [docs/naming.md](docs/naming.md).

---

## Install

```sh
# one-line installer (macOS/Linux, arm64/x86)
curl -fsSL https://raw.githubusercontent.com/ghostlygawd/amber/main/install.sh | sh

# or Homebrew
brew install ghostlygawd/tap/amber

# or from source (Go 1.24+)
go install github.com/ghostlygawd/amber/cmd/amber@latest
```

The binary is under 25 MB. The optional local embedding model is a
separate ~30 MB download offered at `init`; without it, Amber runs on
exact + BM25 lexical recall (the "floor").

## Two minutes to your first memory

```sh
amber init                      # create ~/.amber, set up embeddings
amber remember "We deploy the billing service to Fly.io on Fridays" --type decision
amber recall "where does billing deploy"
```

## Ten minutes to a populated store

Amber can build an initial store from history you already own — your
local Claude Code transcripts — and from your existing memory files.
**Nobody else does this.**

```sh
amber digest --transcripts 30d  # digest the last 30 days of sessions
amber review                    # approve / edit / reject what it learned
```

## Wire it into Claude Code

```sh
amber hooks install             # SessionStart recall + SessionEnd digest
```

- **SessionStart** injects a token-budgeted briefing (≤700 tokens by
  default, ~1% of a session), framed as *data, not instructions*, and
  deduped against your CLAUDE.md.
- **SessionEnd** digests the transcript into new memories, routed through
  the review inbox until you switch the posture to automatic.

Or mount the MCP server from any client:

```sh
claude mcp add amber -- amber serve
```

## Why not just CLAUDE.md?

CLAUDE.md is great — and Amber keeps it. Instructions are what you *tell*
an agent; memory is what it *learns*. The problem isn't the file, it's
the *unreviewed* file: 800 lines of accumulated contradictions no one
audits, a secret pasted into a repo, or an untrusted tool result quietly
writing an instruction into your agent's head.

Amber gives the learned half a review inbox, a provenance trail, a
quarantine for untrusted input, and a committable DECISIONS.md. Same
plain text you own — governed.

The empirical case for heavy memory infrastructure is genuinely
contested ([ETH Zurich's AGENTS.md study](docs/benchmarks.md#the-honest-counter-narrative)
found context files often *don't* improve task success). So Amber does
not compete on accuracy claims. It competes on **control, craft, and
ownership**.

## What makes it different

- **Treats memory as an attack surface.** Trust tiers (T0–T3), a
  declarative-only constraint, taint marking, a quarantine inbox, and a
  poisoning test suite in CI. Content from tool output or web pages is
  quarantined and never auto-injected until you review it. See
  [docs/threat-model.md](docs/threat-model.md).
- **Retroactive onboarding.** A populated, reviewed store from your own
  transcript history in about ten minutes.
- **Competitor-file ingestion.** `amber digest MEMORY.md` / `CLAUDE.md` /
  `AGENTS.md` — a one-command migration path.
- **Auto-maintained DECISIONS.md** and a published, open
  [interchange schema](docs/interchange-schema.json).
- **Recall attribution.** `amber recall --why` shows which memories were
  retrieved, their scores, and why they were included. Nobody else ships
  this.
- **Never auto-deletes.** Consolidation merges, resolves, and demotes —
  but every action is journaled and reversible. Aged memories leave
  auto-injection; they never leave the store.

## The reviewer test

Amber is built to pass all four (the gaps a competitive sweep exposed):

1. Does it consolidate? — `amber consolidate` (D16)
2. Can I see what it remembers? — `amber browse` (D17)
3. Can I see why it injected that? — `amber recall --why` (D18)
4. Does it redact secrets? — secret + PII scan on write and export (§10)

## Command tour

| Command | What it does |
|---|---|
| `amber init` | Create a store; set up embeddings; offer retroactive digest + interview |
| `amber remember <text>` | Store a memory (user-stated, T0); dedupe + supersedence + secret scan |
| `amber recall <query> [--why] [--format context]` | Hybrid semantic + exact search with attribution |
| `amber browse` | TUI: search, filter, inspect; view chains and trust tiers |
| `amber review` | Approve / edit / reject quarantined and auto-digested memories |
| `amber show <id\|entity>` | Full record, or an entity dossier |
| `amber forget <id \| --query \| --entity>` | Soft-delete (tombstone); `--entity` is the erasure primitive |
| `amber digest [file] [--transcripts 30d]` | LLM extraction from a transcript or memory file |
| `amber consolidate [--dry-run]` | Merge, resolve, absolutize dates, demote, re-index — never delete |
| `amber serve` | MCP server (stdio) |
| `amber export [--format jsonl\|md\|decisions]` / `import` | Portability; open interchange schema |
| `amber status` / `config` / `doctor` | Stats, config, integrity + migration |

Every command supports `--format json` where output exists; every
interactive step has a non-interactive flag; exit codes are script-safe
(`0` ok, `1` error, `2` refused by policy).

## Honest limitations

- **Semantic recall depends on a small static model.** It is not a
  large embedding model. On the offline floor (hash + BM25) it scores
  88% top-3 on our 50-query set — good, not perfect. Real numbers,
  including losses, are in [docs/benchmarks.md](docs/benchmarks.md).
- **No cloud sync, no team server in v1.** Team memory is
  committable plain-text export today (PR-reviewable), not live sync.
- **Not a RAG tool.** Amber stores what the agent *experienced*, not
  what documents *say*. No AST/code-graph parsing, no multi-hop graph
  retrieval, no bi-temporal validity windows — [consciously rejected for
  v1](docs/decisions/DECISIONS.md), not overlooked.
- **The name is provisional.** See above.

## Design & decisions

The full build spec, decision log, and strategy live in
[`docs/`](docs/). Start with [the decision log](docs/decisions/DECISIONS.md)
and [the threat model](docs/threat-model.md). The extraction prompt is
[published verbatim](docs/prompts/extract.md). Benchmarks are published
with methodology, judge prompts, seeds, and losses.

## License

MIT. See [LICENSE](LICENSE).
