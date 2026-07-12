# Problem map — the receipts behind the positioning

[positioning.md](positioning.md) names three pains and claims Amber
fills the gap between them. This page is the evidence: what exists
today, what's documented to be broken, and what remains an assumption.

**Method.** Compiled 2026-07-12 from a fan-out research pass over
primary sources (vendor docs, arXiv, incident reports), with each
claim adversarially verified by three independent checks against the
live source; 24 of 25 top claims survived. Where evidence did *not*
survive verification, this page says so instead of quoting it.

## The landscape: memory in coding agents, July 2026

Memory has gone native everywhere. All four major coding agents now
ship it — and every one follows the same design: **the agent decides
what to save; your only recourse is auditing after the fact.**

| System | Storage | Review *before* save | Audit after | Injection provenance | Portable across agents |
|---|---|---|---|---|---|
| **Claude Code auto memory** (on by default, v2.1.59+) | Local markdown, per repo per machine, no sync | **No** — "It decides what's worth remembering" ([docs](https://code.claude.com/docs/en/memory)) | `/memory` browse/edit/delete | No | No |
| **OpenAI Codex memories** (off by default) | Local files under `~/.codex/memories/` | **No** — files are "generated state"; OpenAI says don't rely on hand-editing them ([docs](https://developers.openai.com/codex/memories)) | Coarse on/off toggles | No | No |
| **GitHub Copilot Memory** (on by default for Pro since 2026-03) | GitHub-hosted, repo-scoped, 28-day expiry | **No** user gate (agent-side citation checks) | View/delete in settings | **Partial** — file:line citations ([engineering post](https://github.blog/ai-and-ml/github-copilot/building-an-agentic-memory-system-for-github-copilot/)) | No |
| **Windsurf Cascade memories** | Local per-workspace | **No** — auto-saved mid-conversation ([docs](https://docs.devin.ai/desktop/cascade/memories)) | Edit in settings panel | No | No — docs point teams back to rules files |
| **OpenMemory (CaviraOSS)**, OSS layer | Self-hosted SQLite/Postgres | **No** — writes persist immediately ([repo](https://github.com/CaviraOSS/OpenMemory)) | CLI + dashboard | **Partial** — "waypoint" traces | MCP across clients |
| **The file practice** (CLAUDE.md / AGENTS.md / rules) | In the repo (the good part) | n/a — hand-written | Git | Injected wholesale, no attribution | The de-facto interchange — which is why vendors point durable rules here |
| **Amber** | Local SQLite you own | **Yes — review inbox; quarantine for untrusted origins** | `browse`, journaled ops, reversible | **Yes — `recall --why` per-injection attribution** | Export/import, open schema, ingests the file practice |

Honest scope note: Cursor, Mem0, Zep, Letta, LangMem, cognee, and
Supermemory did not yield claims that survived verification in this
pass; the map above covers what did. Treat their cells as unknown, not
as gaps.

## What the verified evidence says about each pain

### P1 — Groundhog Day (agents forget between sessions)

- **The market's revealed belief:** between 2025 and 2026, Anthropic,
  OpenAI, GitHub, and Windsurf all built session memory. Nobody
  ships a feature this fast unless the forgetting hurts.
- **Peer-reviewed measurement:** LongMemEval (ICLR 2025,
  [arXiv 2410.10813](https://arxiv.org/abs/2410.10813)) found
  commercial assistants drop ~30% in accuracy recalling information
  across sustained multi-session interactions (e.g. ChatGPT-with-memory
  57.7% vs 91.8% when reading the same history directly).
- **Demand signal:** users file requests for memory that survives
  machines and sessions (e.g.
  [claude-code #56793](https://github.com/anthropics/claude-code/issues/56793),
  cross-machine sync).

### P2 — Rot (accumulated context goes stale and backfires)

- **The controlled experiment:** ETH Zurich's AGENTS.md study
  ([arXiv 2602.11988](https://arxiv.org/abs/2602.11988)) — details in
  [benchmarks.md](benchmarks.md#what-the-evidence-against-context-files-actually-says).
  Static context files often didn't improve task success, raised
  inference cost >20%, and LLM-generated ones hurt in 5 of 8 settings.
- **The vendor's own admission:** Anthropic's memory docs state
  CLAUDE.md carries "no guarantee of strict compliance," that files
  over ~200 lines "may reduce adherence," and that contradictory rules
  are resolved "arbitrarily"
  ([docs](https://code.claude.com/docs/en/memory)). The rot failure
  mode is documented by the people shipping the file format.
- **The stale-memory failure, measured:** two of LongMemEval's five
  tested abilities — knowledge updates and abstention — are exactly
  the superseded-fact and answering-from-absent-memory failures, and
  they're where systems scored worst.
- **Governance demand:**
  [claude-code #34776](https://github.com/anthropics/claude-code/issues/34776)
  requests memory governance for long-running users — corrections that
  accumulate without expiry across projects.

### P3 — Shadow state (memory you don't control)

- **Review-before-save exists nowhere in the surveyed set.** All five
  systems above write autonomously; two vendors document it outright
  (Anthropic: "It decides what's worth remembering"; OpenAI: memories
  are "generated state").
- **Users asking for control:**
  [claude-code #23544](https://github.com/anthropics/claude-code/issues/23544)
  and [#23750](https://github.com/anthropics/claude-code/issues/23750)
  ask for the ability to disable auto-memory — the demand isn't "give
  me memory," it's "give me a say in it."
- **Poisoning is an incident, not a hypothetical:** the SpAIware
  exploit achieved persistent data exfiltration on Windsurf by having
  an indirect prompt injection invoke the memory tool "without a human
  having to approve it"
  ([Embrace The Red](https://embracethered.com/blog/posts/2025/windsurf-spaiware-exploit-persistent-prompt-injection/)).
- **Lock-in is universal:** no surveyed memory store moves across
  agents; OpenAI and Windsurf docs both direct durable, shareable
  rules *back into checked-in files* — conceding that the portable
  layer is plain text you own, which is Amber's storage philosophy.

## Pain → best existing answer → verified gap → Amber

| Pain | Best existing answer | The gap (verified) | Amber |
|---|---|---|---|
| Re-explaining every session | Native auto-memory (all four vendors) | Recall is unreliable (LongMemEval −30%); stores are per-machine/per-workspace silos | Session-start briefing from a store you can also build *retroactively* from transcript history |
| Context file bloat/rot | Hand-pruning CLAUDE.md | No tooling: no dedupe, no supersedence, no aging; adherence degrades past ~200 lines by vendor admission | Review inbox, dedupe, supersedence, `consolidate`, ≤700-token budget, stale entries age out of injection |
| Wrong/unwanted memories saved | Post-hoc delete (all vendors) | **No system offers approval before save** | The review inbox *is* the product: approve/edit/reject before anything is believed |
| "Why did it say that?" | Copilot citations; OpenMemory waypoints | No user-facing per-injection explanation in Claude Code/Codex/Windsurf | `recall --why`: every injected memory, its score, its reason |
| Memory poisoning | Copilot's agent-side citation checks (self-reported) | Demonstrated exploit on unattended saves (SpAIware); no quarantine anywhere | Trust tiers, taint marking, quarantine, declarative-only constraint, poisoning suite in CI |
| Lock-in / can't take it with you | Checked-in AGENTS.md (rules only, not learned memory) | Learned memory is never portable | Open interchange schema, export/import, one-command ingestion of existing memory files |

## The efficacy question, answered honestly

Does injected memory make agents *better*? The verified evidence is
split, and the split is informative:

- **For:** GitHub's vendor-run A/B reports PR merge rates rising
  83%→90% with Copilot Memory (p<0.00001) — self-reported, methodology
  unpublished, no independent replication.
- **Against:** ETH's controlled study found static context files
  don't generally help and cost >20% more; LongMemEval found
  production memory systems failing multi-session recall.
- **The reconciliation, per ETH's own split result:** agents *do*
  follow explicit, specific instructions in context (measured up to
  ~2.5× behavioral compliance); what doesn't help is bulk descriptive
  overview. Specific, verified, instruction-shaped content earns its
  tokens; hoarded context doesn't.

Amber's design takes the reconciliation seriously (small, specific,
reviewed, budgeted) and still refuses to promise uplift: `recall
--why` and the visible injection budget exist so each user can measure
whether memory earns its keep in *their* workflow — and turn it off if
it doesn't.

## What is NOT yet validated (kept here so we can't forget it)

1. **Verbatim user complaints.** No Reddit/HN/issue-thread quote
   survived adversarial verification in this pass. The complaint
   *categories* are validated by vendor admissions, measurements, and
   incidents above — but the quotable voice-of-user evidence is
   pending. The Week-0 interviews ([week0-gate.md](week0-gate.md))
   are that collection pass; source them from the verified-live
   threads (#23544, #23750, #34776, #56793). Issue #38536, cited in
   earlier planning, did not surface in verification — confirm it
   exists before citing it again.
2. **Willingness to adopt.** Pains being real does not prove people
   will install a tool for them. The landing-page conversion and
   unprompted-pain interview gates in week0-gate.md remain the test,
   with kill criteria unchanged.
3. **That Amber's reviewed injection improves outcomes.** Unproven by
   design — we ship the instrument, not the claim.
4. **Scope of "local-first."** It means storage, recall, and review
   run locally with no account. Briefing content injected into an
   agent still reaches that agent's model provider at inference time,
   like anything else in your prompt.

## Primary sources

- Claude Code memory docs — https://code.claude.com/docs/en/memory
- OpenAI Codex memories docs — https://developers.openai.com/codex/memories
- GitHub Copilot memory engineering post — https://github.blog/ai-and-ml/github-copilot/building-an-agentic-memory-system-for-github-copilot/
- Windsurf Cascade memories docs — https://docs.devin.ai/desktop/cascade/memories
- ETH Zurich, *Evaluating AGENTS.md* — https://arxiv.org/abs/2602.11988
- LongMemEval (ICLR 2025) — https://arxiv.org/abs/2410.10813
- SpAIware exploit write-up — https://embracethered.com/blog/posts/2025/windsurf-spaiware-exploit-persistent-prompt-injection/
- OpenMemory (CaviraOSS) — https://github.com/CaviraOSS/OpenMemory
