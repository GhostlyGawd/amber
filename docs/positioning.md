# Positioning — the single source of truth for everything we say

Every user-facing sentence in this project — README, website, CLI
output, docs, release notes — must trace back to this page. Marketing
is not a launch artifact; it is the product experience. If a sentence
here or anywhere else doesn't obviously connect to a user's problem,
it is wrong and gets cut.

## The problem, in one sentence

> Your coding agent forgets everything between sessions — and the two
> standard fixes, a growing CLAUDE.md or the vendor's black-box
> auto-memory, replace forgetting with something worse: a pile of
> stale, unreviewed context that costs tokens, can make output worse,
> and isn't yours.

That sentence contains the three pains. Every feature must map to one
of them; every sentence of copy must connect to one of them.

## The three pains (named, so copy can reference them)

| # | Pain | What the user experiences | Evidence |
|---|------|---------------------------|----------|
| P1 | **Groundhog Day** | Re-explaining the stack, the conventions, the decisions — every single session. | Native-memory demand threads (anthropics/claude-code #23544, #23750, #34776, #38536); customer research rated this STRONG. See [problem-map.md](problem-map.md). |
| P2 | **Rot** | The DIY fix for P1 — a fat CLAUDE.md / AGENTS.md — accumulates contradictions and stale facts nobody audits. The evidence says this actively backfires: context files often don't improve task success, raise cost, and LLM-generated ones can make results worse. | ETH Zurich AGENTS.md study; see [benchmarks.md](benchmarks.md#what-the-evidence-against-context-files-actually-says). |
| P3 | **Shadow state** | The vendor fix for P1 — native auto-memory — writes things you can't fully inspect, port, or delete, and anything the agent reads can write into it (poisoning). Your accumulated context is also locked to one tool. | Threat model + poisoning literature; see [threat-model.md](threat-model.md) and [problem-map.md](problem-map.md). |

P1 is why people want memory. P2 and P3 are why the existing answers
to P1 fail. Amber exists in that gap.

## The answer, in one sentence

> Amber is reviewed memory: it learns from your sessions, **you
> approve what it keeps**, and it injects a small, budgeted, current
> briefing it can show you — stored in one local SQLite file you own
> and can take to any agent.

## Pillar → pain mapping (features may only be marketed via a pillar)

| Pillar | Answers | The features behind it |
|--------|---------|------------------------|
| **Remembers for you** | P1 | SessionStart briefing; `digest --transcripts` (a populated store from history you already own, in ~10 minutes); MCP server for any client |
| **Curated, not hoarded** | P2 | Review inbox; dedupe + supersedence; `consolidate`; ≤700-token injection budget; stale memories age out of injection (never out of the store) |
| **Yours to read, audit, and take** | P3 | Plain SQLite + open interchange schema + export/import; `recall --why` attribution; trust tiers + quarantine for untrusted input; poisoning suite in CI; no account, no cloud, no telemetry |

If a feature can't be expressed through a pillar, we don't market it.

## The evidence paragraph (canonical — reuse verbatim, don't improvise)

The ETH Zurich AGENTS.md study is not a caveat to bury; it is the
design brief. The canonical framing:

> Researchers at ETH Zurich tested the most common fix — a static
> context file injected into every prompt — and found it often doesn't
> improve task success, raises inference cost, and that LLM-generated
> context files made results *worse* in most tested settings. That
> isn't evidence against memory. It's a measurement of what happens
> when memory is **unreviewed, unbudgeted, and never pruned** — the
> exact failure Amber is built to prevent. It is why Amber reviews
> before it keeps, budgets what it injects, retires what goes stale,
> and shows you what was injected (`recall --why`) so "did it help?"
> is answered with your data, not our claim.

## What we claim / what we refuse to claim

**We claim:**

- You stop re-explaining your project every session. (P1)
- Your agent's briefing stays small, current, and deduplicated —
  because you reviewed it. (P2)
- You can read every memory, see why each one was injected, export
  everything, and delete anything. Untrusted content never reaches
  your agent's head without your approval. (P3)
- Our numbers are published with methodology, seeds, and losses.

**We refuse to claim:**

- "Memory makes your agent smarter" or any benchmark-accuracy uplift
  from injection. The evidence is contested; instead we ship the
  instrument (`recall --why`, injection visible in `status`) so each
  user can measure it — and turn it off if it doesn't earn its tokens.
- Anything quantitative that isn't in [benchmarks.md](benchmarks.md)
  or a cited external source.

## The sentence test (editorial gate for ALL user-facing text)

Before any sentence ships, it must pass all three:

1. **Pain-traceable.** Which of P1/P2/P3 does it connect to? None →
   cut it.
2. **Outsider-readable.** Would a developer meeting Amber for the
   first time understand it without our internal vocabulary? Internal
   jargon ("heavy memory infrastructure", hypothesis codes, tier
   numbers without explanation) stays in internal docs.
3. **True and checkable.** Every number traces to benchmarks.md or a
   linked source. No invented quotes, no unverifiable superlatives.

## Voice

- Say **remember, learn, review, approve, briefing, own, inspect**.
- Avoid **semantic layer, knowledge graph, RAG pipeline,
  bi-temporal, infrastructure** in marketing surfaces — they describe
  mechanism, not relief.
- Calm, specific, a little dry. Wit only where it earns trust.

## Audience

- **Wedge (now):** individual developers living in Claude Code /
  Cursor / Codex daily — feeling P1 weekly and P2 already.
- **Business (later):** teams, where P3 becomes governance — shared
  reviewed memory, provenance, poisoning defenses. Per
  [week0-gate.md](week0-gate.md): the team tier is the business;
  security is how we earn the right to sell it.

## Status of validation (kept honest, updated as evidence arrives)

The pains above are grounded in public demand threads and the cited
study; the **willingness to adopt this solution is not yet
proven**. The gate in [week0-gate.md](week0-gate.md) (landing-page
conversion + 10 unprompted-pain interviews, with kill criteria)
remains the test before scaling GTM spend. The cited-evidence map
lives in [problem-map.md](problem-map.md).
