# Amber

**Local-first, long-term memory for AI coding agents.** One static Go
binary, one SQLite file you own. No Docker, no API key, no account,
offline by default.

## The problem

Every session, your coding agent starts from zero. The stack, the
conventions, the decision you explained on Tuesday — gone. So you
explain it again.

The two standard fixes trade forgetting for something worse:

- **The ever-growing context file.** CLAUDE.md / AGENTS.md swells into
  hundreds of lines of stale facts and contradictions that nobody
  audits — and that get pushed into *every* prompt. Tested
  empirically, this backfires: static context files often fail to
  improve task success while raising cost, and LLM-generated ones can
  make results actively worse
  ([what the evidence says](docs/benchmarks.md#what-the-evidence-against-context-files-actually-says)).
- **The vendor's auto-memory.** The agent quietly writes things about
  you and your codebase into a store you can't fully read, export, or
  audit — locked to one tool. And anything the agent reads (tool
  output, a web page) can write into what it believes.

## What Amber does

Amber is **reviewed memory**. It learns from your sessions, **you
approve what it keeps**, and it injects a small, budgeted, current
briefing — from one SQLite file you own and can take to any agent.

- **Remembers for you.** A session-start briefing so you stop
  re-explaining; a populated store built from transcript history you
  already have, in about ten minutes.
- **Curated, not hoarded.** A review inbox, dedupe, contradiction
  handling, and a ≤700-token injection budget (~1% of a session).
  Stale memories age out of the briefing — never out of the store.
- **Yours to read, audit, and take.** Plain SQLite, an open export
  format, `recall --why` showing exactly what was injected and why,
  and quarantine for anything that came from an untrusted source.

> Instructions are what you *tell* an agent (CLAUDE.md / AGENTS.md).
> Memory is what it *learns* (Amber). Amber keeps your CLAUDE.md —
> and governs the learned half.

> **Name notice.** "Amber" is provisional — it collides with
> amber-lang.com and others. Everything is `amber` / `~/.amber/` /
> `AMBER_*` so a rename is a mechanical find/replace. See
> [docs/naming.md](docs/naming.md).

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

## Ten minutes to a store that already knows your project

You've already explained your project to your agent — in past
sessions. Amber can read that history back and turn it into reviewed
memories, so day one doesn't start from zero:

```sh
amber digest --transcripts 30d  # digest the last 30 days of sessions
amber review                    # approve / edit / reject what it learned
```

## Wire it into Claude Code

```sh
amber hooks install             # SessionStart recall + SessionEnd digest
```

- **SessionStart** injects the briefing: token-budgeted (≤700 by
  default), framed as *data, not instructions*, deduped against your
  CLAUDE.md so nothing is said twice.
- **SessionEnd** digests the session into proposed memories, routed
  through the review inbox until you choose to trust it on autopilot.

Or mount the MCP server from any client:

```sh
claude mcp add amber -- amber serve
```

## Why a review step? (what the evidence says)

Researchers at ETH Zurich tested the most common memory practice — a
static context file injected into every prompt — and found it often
doesn't improve task success, raises inference cost, and that
LLM-generated context files made results *worse* in most tested
settings ([details and citation](docs/benchmarks.md#what-the-evidence-against-context-files-actually-says)).

That result is not an argument against memory. It is a measurement of
what happens when memory is **unreviewed, unbudgeted, and never
pruned** — and it is the design brief Amber is built from:

- **review before it's kept** (`amber review`),
- **budget what's injected** (≤700 tokens, visible in `status`,
  deduped against CLAUDE.md),
- **retire what goes stale** (`amber consolidate` demotes; never
  deletes),
- **show your work** (`amber recall --why`) — so "did it help?" is
  answered with your data, not our claim.

The same study points at what *does* work: agents reliably followed
explicit, specific instructions — the dead weight was bulk repository
overview. So Amber stores decisions, preferences, and corrections,
not summaries of your codebase.

We deliberately make no benchmark-accuracy promises about injected
memory. If it isn't earning its tokens in your workflow, Amber is the
one memory tool that will show you that too.

## Why not just the vendor's built-in memory?

Native auto-memory answers "the agent forgets" — but it creates the
problems Amber refuses to have:

- **You can't fully see it.** Amber is one SQLite file; open it with
  any tool. `amber browse` gives you an inspector; `recall --why`
  shows every injection.
- **You can't take it with you.** Amber exports everything
  (`jsonl`, markdown, a committable DECISIONS.md) under an
  [open interchange schema](docs/interchange-schema.json) — and can
  *ingest* your existing MEMORY.md / CLAUDE.md / AGENTS.md as a
  one-command migration.
- **It believes what it reads.** Content from tool output or web
  pages is quarantined in Amber — tiered by trust, marked as tainted,
  never auto-injected until you approve it, with a poisoning test
  suite in CI. See [docs/threat-model.md](docs/threat-model.md).
- **It's a black box on someone else's roadmap.** Amber is MIT, local,
  offline by default, telemetry-off.

## Four questions to ask any memory tool

1. Can I see everything it remembers? — `amber browse`
2. Can I see exactly what it injected, and why? — `amber recall --why`
3. Does it clean up after itself — merge duplicates, resolve
   contradictions, retire stale facts — without silently deleting? —
   `amber consolidate`
4. Does it keep secrets out? — secret + PII scan on write and export

Amber is built to answer yes to all four. Ask the same four of
anything else holding your context.

## Command tour

| Command | What it does |
|---|---|
| `amber init` | Create a store; set up embeddings; offer retroactive digest + interview |
| `amber remember <text>` | Store a memory (user-stated, highest trust); dedupe + supersedence + secret scan |
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
  retrieval — [consciously rejected for v1](docs/decisions/DECISIONS.md),
  not overlooked.
- **The name is provisional.** See above.

## Design & decisions

What we say and why we say it: [docs/positioning.md](docs/positioning.md).
The pain-point → existing-solutions → gap map, with sources:
[docs/problem-map.md](docs/problem-map.md). The decision log is
[docs/decisions/DECISIONS.md](docs/decisions/DECISIONS.md), the threat
model [docs/threat-model.md](docs/threat-model.md), and the extraction
prompt is [published verbatim](docs/prompts/extract.md). Benchmarks are
published with methodology, judge prompts, seeds, and losses.

## License

MIT. See [LICENSE](LICENSE).
