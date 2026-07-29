# Security policy

Amber stores durable agent memory and treats memory poisoning, secret leakage,
and provenance errors as security defects.

## Supported versions

Amber has no published release yet. Security fixes currently target the `main`
branch. This statement does not make `main` a stable or production-supported
release.

## Report a vulnerability

Use GitHub's private vulnerability-reporting flow for this repository when it
is available. Do not open a public issue for a vulnerability that contains an
exploit, credential, private memory, or other sensitive data.

Include:

- the affected commit;
- the trust tier, origin, and command or MCP path involved;
- a minimal reproduction with synthetic data;
- the observed result and the expected trust boundary; and
- whether the report concerns injection, persistence, export, or deletion.

Never include a real secret or a user's actual memory database. Replace all
sensitive values with deterministic test fixtures.

The threat model is documented in [docs/threat-model.md](docs/threat-model.md).
Privacy and deletion semantics are documented in
[docs/privacy.md](docs/privacy.md).
