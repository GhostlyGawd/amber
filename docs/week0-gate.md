# Week 0 — validation gate (do not skip)

Five working days, **no code**. The build is gated on this. (The code in
this repo exists because the gate is assumed passed for the exercise;
in a real launch you run the gate first.)

## What to run

1. **Landing-page smoke test.** A/B two value props:
   - "one memory across every agent"
   - "memory that doesn't rot"
   Drive traffic; measure visitor → waitlist conversion.
2. **10 interviews** sourced from the native-memory issue threads
   (anthropics/claude-code #23544, #23750, #34776, #38536). Ask about the
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
