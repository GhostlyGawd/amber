# Amber first-use dogfood workpad

Date: 2026-07-30

Source commit: `f70774cd54a7a3579942b6953936204c67daf989`

Scope: run the documented source-install and first-memory path in an isolated
Linux environment, correct verified onboarding friction, and preserve Amber's
existing release, privacy, and runtime boundaries.

Completion state: `self-contained`

External gates: none for this pull request. Actual user adoption remains an
ongoing product objective.

Evidence destination: this workpad.

Terminal action: `keep-draft` for owner review after pull-request CI passes.

## Alignment

| Contract item | Normative level | Implementation | Test | Docs/example | Status |
|---|---|---|---|---|---|
| The documented source install produces the current Amber command with Go 1.25 or newer | Required | Installed `github.com/ghostlygawd/amber/cmd/amber@latest` with Go 1.26.5 in a clean HOME, GOPATH, and build cache | The command completed and produced a 20,395,156-byte Linux amd64 binary | README keeps source installation and the no-release boundary | Aligned |
| A user can find the installed command after `go install` | Required | Kept Go's standard install destination and documented its PATH requirement | The clean shell could not find `amber` until `$(go env GOPATH)/bin` was added to PATH | README and website now state the command-discovery step | Implemented |
| The two-minute path reaches a first reviewed memory without setup prompts | Required | Uses `amber init --defaults` for BM25-only, review-first, telemetry-off initialization | Clean init, remember, recall with attribution, JSON status, and doctor passed | README and website quickstart use the same command | Implemented |
| Volatile first-run measurements stay in dated evidence | Required | Recorded the cold-build observation here, not in timeless setup claims | Clean source install elapsed 248.35 seconds on this host | Public guidance says only that a cold build can take several minutes | Aligned |
| Release and readiness claims do not increase | Required | No binary release, tag, installer publication, or repository setting changed | Repository and claim review | README still says no published binary or Homebrew release | Aligned |

## Clean-environment method

The test used task-specific directories under `/home/rhenm/scratch` for HOME,
GOPATH, GOCACHE, and the sample project. The environment excluded the installed
dogfood binary and the real Amber project store. Go 1.26.5 satisfied the
repository's Go 1.25 minimum.

The exact source-install command completed from the public default branch. A
cold cache required 248.35 seconds and produced a 20,395,156-byte binary. Go
placed that binary under GOPATH, but the clean PATH did not include GOPATH's
`bin` directory. That command-discovery failure is the verified onboarding
problem corrected by this change.

After adding GOPATH's `bin` directory to PATH, the following path passed:

1. `amber init --defaults` created a BM25-only store with review-first posture,
   telemetry off, and no prompts;
2. `amber remember` stored one T0 decision;
3. `amber recall --why` returned the decision with lexical attribution;
4. `amber status --format json` reported one active memory, no embedding
   provider, and telemetry off; and
5. `amber doctor` reported no issues.

The revised landing page was also inspected at 1280 × 900 and 390 × 844. The
install note was visible at both sizes, the mobile page had no horizontal
overflow, and the quickstart contained `amber init --defaults`. The copy
control remained present. The isolated browser did not grant clipboard access,
so this run does not claim end-to-end clipboard success; the existing
selection fallback was unchanged.

A pseudo-terminal without terminal device responses stalled during color
probing. Repeating the interactive check with `TERM=dumb` completed normally.
This was classified as a harness limitation because an ordinary terminal
responds to those device queries; no runtime change follows from it.

## Drift classification

- **Documentation-only drift corrected:** the source-install instructions did
  not explain the cold-build delay or Go's GOPATH command location.
- **Documentation-only drift corrected:** the public two-minute path invoked
  interactive setup even though the documented agent-safe path already offered
  deterministic review-first defaults.
- **Reviewed and unaffected:** CLI runtime behavior, database schema,
  architecture, security scanning, privacy, telemetry defaults, release state,
  version, benchmark claims, and GitHub settings did not change.
- **Unresolved product limitation:** a packaged binary would remove the cold Go
  build and PATH friction, but publishing a release remains outside this task
  and requires a separate owner decision.
