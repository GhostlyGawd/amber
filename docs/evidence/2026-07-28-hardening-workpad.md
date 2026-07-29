# Amber trust and portability hardening workpad

Date: 2026-07-28

Scope: repository-local fixes approved after the review of commit
`3ff09d15f5e292dafd8b1b604a92c768b8bb4651`. Release publication and
GitHub settings remain outside this workpad.

| Contract item | Normative level | Implementation | Test | Docs/example | Status |
|---|---|---|---|---|---|
| Secret and PII redaction never leaves a matched secret in forced writes or exports | Required | Ordered, overlap-safe redaction in `internal/scan` | Mixed-kind and overlapping redaction regression tests | Threat model and privacy posture | Implemented |
| MCP provenance fails closed when the caller omits or cannot verify origin | Required | Central origin classification in `internal/mcpserver` | Omitted, inferred, untrusted, and invalid origin tests | MCP schema, AGENTS, threat model | Implemented |
| Untrusted T3 memory can become active only through review approval | Required | Restore keeps T3 quarantined | Restore-state regression test | CLI help and threat model | Implemented |
| Check, write, supersedence, model pinning, flags, and journal updates are atomic | Required | `Store.AtomicWrite` transaction boundary | 24-writer repeated concurrency test and rollback test | Storage schema | Implemented |
| Configured embedding-model migration can run from a mismatched store | Required | Separate migration embedder plus atomic vector replacement | Searchable-row success and migration rollback tests | Doctor help and storage schema | Implemented |
| JSONL export/import preserves the documented graph and timestamps | Required | Validated two-pass import through `Store.ImportBatch` | Graph, alias, tag, timestamp, idempotence, and invalid-time tests | Interchange schema and privacy docs | Implemented |
| Forget is described as reversible soft deletion, not physical erasure | Required | No behavior expansion in this change | Existing tombstone and restore tests | Privacy, README, CLI help | Aligned |
| Go runtime requirements agree across module, CI, setup, and release surfaces | Required | Module and CI use Go 1.25 | Local Go 1.25 full-suite verification | README, AGENTS, CONTRIBUTING | Aligned |
| Installation claims name only paths that exist now | Required | Installer fails closed without checksums; no release published | Shell syntax, fail-closed branch review, and source build | README, AGENTS, landing page | Aligned |
| Public product evidence remains truthful | Recommended | No generated evidence without a verified capture | Landing-page and evidence-claim review | Validation gate marked unrun; visual capture remains open | Reviewed, gap retained |

## Evidence

- Baseline `go test ./... -count=1`: passed with verified Go 1.25.0 before changes.
- Live head CI for the reviewed commit: passed build, vet, unit and acceptance
  suites, size budget, and tier-1 cross-compilation.
- Local Codeweb mapping was unavailable because the Windows-hosted mapper cannot
  access the required Linux-native checkout without a prohibited UNC path.
- Focused trust tests passed for `internal/scan`, `internal/mcpserver`, and
  `internal/cli`.
- The concurrent duplicate-write test used 24 writers and passed 10 consecutive
  runs with one stored memory per run.
- Store rollback tests cover partial belief writes and failed embedding-model
  migrations.
- JSONL tests preserve original IDs, `created_at`, `updated_at`,
  `superseded_by`, entity aliases, and tags. Invalid timestamps write no rows.
- On the final source state, `go vet ./...` and `go test ./... -count=1`
  passed with the verified Go 1.25.0 toolchain.
- Race-enabled writer/store concurrency and rollback tests passed.
- The final CGO-disabled binary was 19,900,876 bytes, below the 25 MiB CI
  budget.
- `sh -n install.sh`, JSON schema parsing, `gofmt`, and `git diff --check` passed.
- Documentation drift classifications: corrected behavior claims in the
  threat model, schema, privacy, interchange schema, and command help;
  corrected setup and release-state claims in README, AGENTS, CONTRIBUTING,
  CI, installer, and landing page; corrected evidence claims in benchmarks and
  the Week 0 gate.
- Reviewed without behavioral changes: positioning, problem map, decision log,
  consolidation guide, naming plan, and release configuration.
- No screenshot or product demo was fabricated. A sanitized visual capture is
  still required before product-readiness claims.
