# Decisions

The build-spec decision log (D1–D18) and the founder register (F1–F4).
This is a hand-maintained ADR; `amber export --format decisions`
generates the *runtime* DECISIONS.md from decision-type memories in a
store — a different artifact for a different purpose.

## Engineering decisions

| # | Decision | Choice | Rationale |
|---|---|---|---|
| D1 | Language | **Go** | Single static binary; trivial cross-compile; official MCP Go SDK |
| D2 | Storage | **SQLite** (pure-Go, no CGO) + FTS5 | Zero infra; user-inspectable; portable |
| D3 | Vector search | **In-process brute-force cosine** over BLOB embeddings | Milliseconds at <100k; no extension |
| D4 | Embeddings | **Local static model** (~30MB, Model2Vec-class, pure-Go); API opt-in; BM25 floor | Offline, private, keyless |
| D5 | Capture | **Explicit `remember` + LLM `digest`** with diff preview | Auto-capture-everything is a privacy footgun |
| D6 | Digest LLM | **`claude -p`** when present; API fallback; configurable | Zero extra setup for Claude Code users |
| D7 | Semantics | **Belief state with supersedence**; soft-delete only | Same semantics at zero infra |
| D8 | Team memory | **Committable plain-text export**, never the DB | VCS-native, PR-reviewable |
| D9 | Name | **`amber`** (provisional — F4) | Metaphor + calm ethos; collision acknowledged |
| D10 | License | **MIT** | Adoption |
| D11 | Benchmarks | **LongMemEval-S in CI; publish with losses, judge prompts, seeds** | The field has an integrity crisis |
| D12 | Platform posture | **Complement, then replace** — digest ingests MEMORY.md / CLAUDE.md / AGENTS.md | Their files are our feedstock |
| D13 | **Threat model** | **Trust tiers, declarative-only, taint, quarantine, poisoning CI** | The one tested threat model in the field |
| D14 | Ops journal | **Append-only operations log in v1** | Makes future sync a merge, not a rewrite |
| D15 | Onboarding | **Retroactive memory** — digest local transcript history at init | Nobody else does this |
| D16 | **Consolidation** | **Background pass, opt-in, reversible, never auto-deletes** | The field ships this; without it we look a generation behind |
| D17 | **Inspection TUI** | **`amber browse`** | Terminal-native browser; gap-filler and on-brand |
| D18 | **Recall observability** | **Show what was injected and why** | Cheap, high-trust; nobody ships it |

## Founder register (blocking)

| # | Decision | Recommendation on file |
|---|---|---|
| **F1** | Endgame | **Bootstrap-first, option-preserving.** Decide at first inflection (~5k installs or first hard team pull) |
| **F2** | Kill-criteria sign-off | Approve Week-0 numbers as written before any code |
| **F3** | Trust posture default | **Review-first for a store's first two weeks, then offer auto.** Teach control, then earn invisibility |
| **F4** | Name | **Rename before launch.** See [naming.md](../naming.md) |

## Consciously rejected for v1

Not overlooked — rejected, and we say so:

- Bi-temporal validity windows (Zep's strength; not our wedge)
- Graph multi-hop retrieval (plain filesystem beat Mem0's graph on LoCoMo)
- AST / code-graph parsing (that's a different tool)
- Live team sync and cloud sync (paid line is at coordination, phase 3)

## How posture F3 shows up in the product

A new store digests with `digest.posture = review-first`: auto-digested
(T2) memories land in the review inbox, not straight into active recall.
After two weeks, `amber status` suggests — once, never nagging —
switching to `auto`. T3 untrusted-origin memories stay quarantined
regardless of posture. This is the "calm because governed" thesis in
code.
