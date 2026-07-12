# Naming (open decision F4)

**"Amber" is provisional.** It collides with:

- **amber-lang.com** — a Bash-transpiled language serving the identical
  HN/developer audience. This is the serious one: permanent SEO dilution
  and "which Amber?" confusion on an organic strategy.
- **amber.ai**, **Amber Smalltalk**, `nineties/amber`.

## Why it's still safe to build under this name

The code treats the name as a variable. Everything is `amber` (binary),
`~/.amber/` (paths), `AMBER_*` (env vars), `amber.v1` (interchange
schema). A rename is a mechanical find/replace across the repo — no
architectural coupling to the string.

## The decision

Rename **before public launch**, not before build. Trigger: if you cannot
get clean `amber` on GitHub + Homebrew + an exact-match domain, switch.
The recommendation on file (§F4) is to rename — one-word, one-color
ownership is the Linear/Raycast/Cursor lesson, and you cannot own
"Amber".

Until then, if the name must appear in public, always qualify it —
"Amber Memory" — with a unique org and exact-match domain.

## Rename checklist (when the time comes)

1. `git grep -il amber` to enumerate.
2. Replace the binary name in `cmd/`, `goreleaser`, `install.sh`, the
   brew formula.
3. Replace path constants: `internal/config` (`~/.amber`, `.amber`,
   `AMBER_*`), `internal/store` (db filename).
4. Bump the interchange schema id if the brand is part of it (keep a
   compatibility alias so old exports still import).
5. Registry listings: `server.json`, `llms.txt`, `AGENTS.md`.
6. Docs and README.
