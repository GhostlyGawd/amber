# Amber documentation

- [Positioning](positioning.md) — the problem we solve, the claims we make (and refuse to make), and the sentence test every user-facing word must pass.
- [Problem map](problem-map.md) — documented user pains → existing solutions → gaps → Amber's answer, with sources.
- [Decision log (D1–D18, F1–F4)](decisions/DECISIONS.md) — what was chosen and why.
- [Threat model](threat-model.md) — memory poisoning and the six defenses.
- [Storage schema](schema.md) — the SQLite tables and the ops journal.
- [Interchange schema](interchange-schema.json) — the open `amber.v1` export format.
- [Extraction prompt](prompts/extract.md) — published verbatim.
- [Benchmarks](benchmarks.md) — methodology, judge prompt, and losses.
- [Privacy & security posture](privacy.md) — telemetry, scanning, permissions.
- [Consolidation](consolidate.md) — the never-delete maintenance pass.
- [Naming (F4)](naming.md) — why "Amber" is provisional.
- [Week 0 gate](week0-gate.md) — the validation gate before code.

The build spec these implement is the master document. The strategy,
competitive, customer, and GTM context live there; this directory is the
engineering-facing subset plus the launch-critical public artifacts.
