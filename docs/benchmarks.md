# Benchmarks

The field has a benchmark-integrity crisis: LoCoMo shipped with ~6.4%
score-corrupting errors and a judge that accepted 62.8% of intentionally
wrong answers; several vendors' headline numbers did not reproduce. Our
posture is the only credible response — **publish the harness, the judge
prompt, the seeds, and the losses, or publish nothing.**

Everything below is reproducible from this repo. Numbers are real; when
they are not great, they say so.

## Internal recall suite (CI-gated)

- Harness: `internal/suites/recall_test.go`
- Data: `testdata/recall_suite.json` — 54 planted memories, 50 queries
  mixing exact keywords and paraphrase.
- Embedder: the **offline floor** (deterministic hash + BM25). The local
  static model can only improve on this.

| Metric | Result | Gate |
|---|---|---|
| top-3 recall | **44/50 (88%)** | ≥80% |

The six misses are logged by the test (`go test -run TestRecallSuite -v`)
— they are paraphrase queries where a shared common word pulled a
distractor above the target. Losses are not hidden; they are the to-do
list.

## Contradiction / belief suite (CI-gated)

- Harness: `internal/suites/contradiction_test.go`
- Data: `testdata/contradictions.json` — 20 update pairs.

| Metric | Result | Gate |
|---|---|---|
| correct supersedence | **20/20 (100%)** | ≥90% |
| hard deletions | **0** | 0 |

## Poisoning suite (CI-gated)

- Harness: `internal/suites/poisoning_test.go`
- Data: `testdata/poisoning/*.jsonl` — tool-output, pasted-web, and
  subtle variants, run through a deliberately gullible extractor.

| Metric | Result | Gate |
|---|---|---|
| active directive memories, unreviewed | **0** | 0 |
| attacks visible in quarantine | yes | yes |

## Recall latency (§7 target: p50 < 50 ms at 50k)

- Harness: `go run ./eval/bench -n 50000 -queries 100`
- Corpus: 50,000 synthetic memories, fixed seed 42, hash-256 vectors.
  This corpus is *adversarial* for lexical search — a tiny vocabulary
  means every term is common — so these numbers are a floor, not a
  best case.

| p50 | p90 | p95 | p99 |
|---|---|---|---|
| **39 ms** | 42 ms | 54 ms | 55 ms |

The first query pays a one-time cache build (~25 ms) plus cold page
cache; steady-state is ~40 ms. Achieved with a lean hot path
(id/vector-only candidate generation, a memoized zero-copy vector
sidecar keyed to the append-only ops epoch, full rows loaded only for
fused finalists) and FTS stopword filtering.

## LongMemEval-S (public)

- Harness: `eval/longmemeval/main.go`
- Judge prompt: `eval/prompts/judge.md` (published verbatim).
- The dataset is not vendored (license); fetch it from the LongMemEval
  repository. Both the answering and judging LLMs are commands you
  control (`-answer-cmd`, `-judge-cmd`), so the run is fully local and
  reproducible.

```sh
go run ./eval/longmemeval \
  -dataset longmemeval_s.json \
  -answer-cmd 'claude -p' \
  -judge-cmd 'claude -p' \
  -out longmemeval-results.json
```

The output JSON contains per-question results **including every loss**,
the seed, and recall-latency percentiles. We publish the results file
alongside each tagged release; we do not cherry-pick question types.

## What the evidence against context files actually says

The most common memory practice today is a static context file
(AGENTS.md / CLAUDE.md) injected into every prompt, growing forever,
reviewed by no one. ETH Zurich's SRI Lab put that practice to the
test — *Evaluating AGENTS.md: Are Repository-Level Context Files
Helpful for Coding Agents?* (Gloaguen, Mündler, Müller, Raychev,
Vechev; [arXiv:2602.11988](https://arxiv.org/abs/2602.11988), v2 June
2026; four agents, SWE-bench Lite plus a novel 138-task benchmark) —
and found:

- context files often did **not** improve task success;
- they raised inference cost by **more than 20%** (the file rides
  along in every prompt, helpful or not);
- LLM-*generated* context files sometimes actively hurt — task
  success **dropped in 5 of 8 settings**.

Read carefully, this is not a finding that "memory doesn't work." The
study never tested reviewed, budgeted, retrieved-on-demand memory —
it tested **unreviewed, unbudgeted, always-injected accumulation**,
and found that it costs real money and can make agents worse. That is
a controlled measurement of the failure mode users already report as
CLAUDE.md bloat and rot.

In other words: the study is the design brief. Each finding maps to a
design rule Amber enforces:

| Study finding | The failure it exposes | Amber's rule |
|---|---|---|
| Files ride along in every prompt, raising cost >20% | No budget, no relevance gate | Injection is **budgeted** (≤700 tokens, ~1% of a session), recalled per-query, visible in `status`, deduped against CLAUDE.md |
| LLM-generated files hurt in 5 of 8 settings | Machine-written context that no human checked | Machine-extracted memories land in a **review inbox** — you approve, edit, or reject before they're ever injected |
| Accumulated context doesn't improve success | Stale and contradictory entries never pruned | `consolidate` merges duplicates, resolves contradictions, and **retires stale memories from injection** (never from the store) |

The same study contains the constructive half of the result: agents
reliably **followed explicit instructions** in context files (invoking
instructed tools at up to ~2.5× baseline), while descriptive
repository overviews — "popular and recommended by model providers" —
were the dead weight. Specific, actionable content earns its context
window; encyclopedic content doesn't. That is why Amber's memory
types are decisions, preferences, corrections, and facts — and why
the briefing is small.

Caveats we carry rather than hide: the study is a preprint;
benchmarks are Python-heavy; two authors are affiliated with a
coding-agent company; developer-written files showed marginal gains
for some agents. And it studies *static context files*, not dynamic
memory systems — strong, adjacent evidence for the rot failure mode,
not a verdict on all memory. (On the dynamic side the evidence is
split: GitHub's vendor-run A/B reports Copilot Memory lifting PR
merge rates 83%→90%, self-reported with unpublished methodology,
while the peer-reviewed LongMemEval — the benchmark we run above —
measured ~30% multi-session recall drops in production assistants.
See [problem-map.md](problem-map.md#the-efficacy-question-answered-honestly).)

One honest consequence remains, and we accept it: if unreviewed
context doesn't reliably improve task success, we will not promise
that reviewed context does. Whether *your* curated 700 tokens earn
their keep is an empirical question about *your* workflow — so Amber
ships the instrument instead of the claim: `recall --why` shows
exactly what was injected and why, and injection is visible in
`status` and can be turned off. If memory isn't helping you, Amber is
the tool that lets you see that.
