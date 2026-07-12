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

## The honest counter-narrative

The empirical case for heavy memory infrastructure is genuinely
contested. The ETH Zurich AGENTS.md study (arXiv:2602.11988) found that
context files often do *not* improve task success while raising inference
cost >20%, and that LLM-generated context files sometimes *hurt* (task
success dropped in 5 of 8 settings).

We take this seriously rather than burying it. It is why Amber:

- **budgets and gates injection** (≤700 tokens, visible in `status`,
  deduped against CLAUDE.md) instead of dumping memory into every prompt;
- **does not compete on raw accuracy claims** — it competes on control,
  audit, and ownership;
- ships `recall --why` so you can see exactly what was injected and
  decide whether it helped.

If memory doesn't help your workflow, Amber makes that measurable too.
