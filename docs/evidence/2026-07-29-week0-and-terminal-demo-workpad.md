# Amber acquisition and terminal demo workpad

Date: 2026-07-29

Source commit: `d635a31cfecbfe935b06a4b702556fe1ad0b137d`

Scope: record a real, sanitized command-line demonstration; optimize the
existing landing page for source adoption; and align repository guidance with
the owner's acquisition-first decision. Release publication and GitHub settings
remain out of scope.

Completion state: `self-contained`

External gates: none for this pull request. User acquisition remains an ongoing
post-merge objective.

Evidence destination: this workpad.

Terminal action: `mark-ready` after implementation, visual verification, and
pull-request CI pass.

## Alignment

| Contract item | Normative level | Implementation | Test | Docs/example | Status |
|---|---|---|---|---|---|
| The documented source build runs with Go 1.25 or newer | Required | Built the merged source with Go 1.26.5 | Clean CGO-disabled build from `d635a31c` | README and CONTRIBUTING retain the Go 1.25 minimum | Aligned |
| The quick-start commands create, store, recall, and inspect one local memory | Required | Ran `init --defaults`, `remember`, `recall --why`, and `status` in an isolated store | All commands exited successfully and produced the recorded output | README includes the recorded terminal demonstration | Implemented |
| Public visual evidence uses real product output and removes private data | Recommended | Rendered the exact sanitized transcript as SVG | Transcript-to-visual content comparison and secret/path scan | SVG, plain-text fallback, caption, date, source commit, and provenance | Implemented |
| The landing page presents the source-adoption path at desktop and mobile page widths | Required | One value proposition, two-step CTA hierarchy, responsive install control, and real demo | Local browser checks at desktop and 390 px mobile widths | Website, README demo, and this dated audit | Implemented |
| Early validation follows the owner's acquisition-first decision | Required | Removed the random headline split and required-interview gate | Strategy and claim review | `docs/week0-gate.md`, positioning, problem map, decision register, and docs index | Aligned |
| Readiness and release claims do not exceed the evidence | Required | No release, production mutation, analytics account, or GitHub settings change | Claim and scope review | Pre-release language remains; adoption signals stay dated and qualified | Aligned |

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
- Visual provenance: `web/assets/amber-terminal-demo.svg` is a deterministic
  text rendering of the recorded output. No generative image model or
  synthetic product output was used.
- Static fallback: `docs/media/amber-terminal-demo.txt` contains the
  sanitized plain-text transcript.
- Rights: the demonstration is repository-authored Amber output and is
  distributed with this MIT-licensed repository.

## Acquisition baseline and current direction

The owner selected product and page optimization before formal interviews on
2026-07-29. Interviews and calls are deferred. The earlier A/B and interview
thresholds are historical planning, not current gates.

GitHub reported this 14-day baseline on 2026-07-29:

- repository views: 0 total, 0 unique;
- repository clones: 11 total, 11 unique;
- stars: 0;
- forks: 0;
- subscribers: 0; and
- referrers: none reported.

These observations are directional. Clones can include automation and are not
proof of users. No conversion, adoption, or market-validation claim follows
from this baseline.

A fresh visual audit of the live page found a coherent amber-and-ink identity,
clear problem framing, and an immediately visible source install command. It
also found three competing calls to action, no real product proof in the first
journey, no keyboard focus treatment, and a mobile install row that placed its
copy control off-screen. The change preserves the existing visual system while
correcting those acquisition and accessibility gaps. Screenshot inspection
cannot establish full keyboard, screen-reader, or WCAG conformance.

## Validation evidence

- Real terminal output and its plain-text fallback parse and match the recorded
  product behavior.
- The Pages workflow publishes the website with the real terminal-demo SVG at
  its local asset path, without a build-time copy or duplicate source.
- The updated page has no random headline assignment, analytics, waitlist, or
  fabricated social proof.
- Desktop and 390 px mobile browser checks cover layout, quick-start anchors,
  demo rendering, and the copy-button state.
- Repository checks cover HTML structure, local asset paths, sensitive text,
  the single alignment table, workflow syntax, and `git diff --check`.
- Pull-request CI remains the authoritative full test run for this change.

## Drift classification

- **Required conflict corrected:** the old mandatory interview and A/B gate
  contradicted the owner's 2026-07-29 acquisition-first decision.
- **Documentation drift corrected:** positioning, the problem map, founder
  decision register, and documentation index now use the same active direction.
- **Recommended product gap corrected:** the landing page now has focused CTA
  hierarchy, a responsive source-install path, visible focus treatment, and
  real product evidence.
- **Reviewed and unaffected:** CLI runtime behavior, storage, architecture,
  interchange, security, privacy, release state, and GitHub settings did not
  change.
