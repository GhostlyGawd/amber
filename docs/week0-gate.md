# Week 0 — validation gate (do not skip)

Five working days. This product-validation gate has not been run. The current
implementation and landing-page variants are engineering artifacts, not proof
that the demand criteria below passed. Do not publish a launch-readiness or
market-validation claim until the owner records the measured outcome here.

## What to run

1. **Landing-page smoke test.** A/B two value props:
   - "one memory across every agent"
   - "memory that doesn't rot"
   Drive traffic; measure visitor → waitlist conversion.
2. **10 interviews** sourced from the native-memory issue threads
   (anthropics/claude-code #23544, #23750, #34776, #56793 — all
   verified live 2026-07; #38536 from the original brief did not
   surface in the verification pass, confirm it exists before using).
   Note what these threads actually are: #23544/#23750 ask to
   *disable* auto-memory and #34776 asks for governance — the demand
   is for control over memory, not just more of it. Ask about the
   pain unprompted before describing Amber.

## Kill criteria (founder-signed, F2)

Reposition or rotate to the fallback bet if **both**:

- visitor → waitlist **< 3%**, **AND**
- **< 6 / 10** interviews describe the pain unprompted.

Either signal strong → build.

## Why this gate and not another

The two uncomfortable truths from the customer research:

- The lead differentiator (memory poisoning, H6) is felt most by people
  who are not yet our users — its felt-demand among individual devs is
  thin. Frame as education; expect the team/enterprise tier to be where
  it converts.
- The first proposed paid tier (individual sync, H7) has **no** direct
  demand evidence. Do not build the business case on it.

Both point the same way: the **team tier is the business; security is how
we earn the right to sell it.** The gate exists to confirm the *free*
wedge (rot + shadow-state + setup friction, all STRONG hypotheses) before
spending three weeks.
