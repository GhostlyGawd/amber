# Amber Week 0 and terminal demo workpad

Date: 2026-07-29

Source commit: `d635a31cfecbfe935b06a4b702556fe1ad0b137d`

Scope: record a real, sanitized command-line demonstration and prepare the
approved Week 0 product-validation work without claiming results that do not
exist. Release publication and GitHub settings remain out of scope.

Completion state: `post-merge-pending`

External gates: five working days of landing-page A/B traffic and 10 human
interviews, as specified in `docs/week0-gate.md`.

Evidence destination: this workpad.

Terminal action: `keep-open` until the external gates have measured results.

## Alignment

| Contract item | Normative level | Implementation | Test | Docs/example | Status |
|---|---|---|---|---|---|
| The documented source build runs with Go 1.25 or newer | Required | Built the merged source with Go 1.26.5 | Clean CGO-disabled build from `d635a31c` | README and CONTRIBUTING retain the Go 1.25 minimum | Aligned |
| The quick-start commands create, store, recall, and inspect one local memory | Required | Ran `init --defaults`, `remember`, `recall --why`, and `status` in an isolated store | All commands exited successfully and produced the recorded output | README now includes the recorded terminal demonstration | Implemented |
| Public visual evidence uses real product output and removes private data | Recommended | Rendered the exact sanitized transcript as SVG | Transcript-to-visual content comparison and secret/path scan | SVG, plain-text fallback, caption, date, source commit, and provenance | Implemented |
| Week 0 measures live conversion and 10 unprompted-pain interviews before a validation claim | Required | No repository implementation can supply human traffic or interviews | No measured traffic or interview results exist yet | `docs/week0-gate.md` remains the normative gate | Pending external evidence |
| Readiness and release claims do not increase before the gate passes | Required | No release, deployment, or settings change in this work | README and Week 0 claim review | Existing pre-release and unvalidated wording remains | Aligned |

## Live terminal evidence

- Runtime: Go 1.26.5 on Linux amd64. This satisfies the documented Go 1.25
  minimum.
- Build: CGO disabled, `-trimpath`, source commit `d635a31c`.
- Store: a new task-specific `AMBER_HOME` with no cached embedding model.
- Commands: `amber --version`, `amber init --defaults`, `amber remember`,
  `amber recall --why`, and `amber status`.
- Result: initialization used the offline BM25 floor; the T0 decision was
  stored; lexical recall returned it with attribution; status reported one
  active memory, an empty review inbox, and telemetry off.
- Sanitization: the exact task-specific store path was replaced with
  `~/.amber`. The sample memory contains no personal, customer, credential,
  or private repository data.
- Visual provenance: `docs/media/amber-terminal-demo.svg` is a deterministic
  text rendering of the recorded output. No generative image model or
  synthetic product output was used.
- Static fallback: `docs/media/amber-terminal-demo.txt` contains the
  sanitized plain-text transcript.
- Rights: the demonstration is repository-authored Amber output and is
  distributed with this MIT-licensed repository.
- Repository validation: XML parse, transcript byte comparison, SVG content
  coverage, sensitive-path and token scan, link targets, alignment-table count,
  and `git diff --check` passed.
- A full local `go test ./...` rerun was stopped after more than seven minutes
  because host load prevented timely completion. Draft PR CI remains required.

## Week 0 current state

The owner authorized the next validation activity on 2026-07-29. Preparation
is complete, but the gate is not running because no traffic source, analytics
destination, or interview contacts are in scope. No conversion or interview
value has been entered.

The next owner must supply or approve:

1. the traffic source and measurement destination for both landing-page value
   propositions;
2. the interview contact list or outreach channel; and
3. the five-working-day start date.

After the run, append the measured visitor, waitlist, conversion, and
unprompted-pain totals here. Apply the existing kill criteria without changing
them after results are known.

## Drift classification

- **Recommended gap corrected:** Amber now has a useful, sanitized CLI visual
  with a plain-text fallback and provenance.
- **Reviewed and unaffected:** setup, security, privacy, architecture,
  interchange, version, and release surfaces did not change because this work
  records existing behavior.
- **Required external evidence pending:** the Week 0 traffic and interview
  measurements remain absent. No market-validation or launch-readiness claim
  is permitted.
