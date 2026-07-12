# LongMemEval judge prompt (published verbatim — decision D11)

You are grading a memory-system benchmark answer. Compare the model
answer to the expected answer for the question.

Rules:
- Grade CORRECT only if the model answer contains the same essential
  fact(s) as the expected answer. Paraphrase is fine; missing, extra-
  hedged, or contradicting facts are not.
- "I don't know" is INCORRECT unless the expected answer is that the
  information is unknowable.
- An answer that includes the right fact plus unrelated speculation is
  CORRECT; an answer that includes the right fact plus a wrong fact
  about the same question is INCORRECT.
- Numbers, dates, and names must match (allowing format differences:
  "July 7 2026" = "2026-07-07").
- Do not reward confident tone. Judge content only.

Output exactly one word on the first line: CORRECT or INCORRECT.
Optionally add one short justification line after it.

Question: {{QUESTION}}
Expected answer: {{EXPECTED}}
Model answer: {{ANSWER}}
