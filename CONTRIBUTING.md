# Contributing

Amber is pre-release software. Contributions must preserve its trust,
portability, and local-first boundaries.

## Development setup

Install Go 1.25 or newer, then run:

```sh
go test ./... -count=1
go vet ./...
```

Run `gofmt` on changed Go files. The CI workflow also checks the release-size
budget and tier-1 cross-compilation.

## Change requirements

- Add a regression test for behavior changes.
- Keep belief adjudication and its write in one store transaction.
- Preserve trust tiers and quarantine unverified origins.
- Do not add telemetry, network access, or a cloud dependency by default.
- Keep JSONL import and export compatible with
  [docs/interchange-schema.json](docs/interchange-schema.json).
- Update the README, command help, schema, threat model, or privacy document
  when their claims change.
- Put dated test totals and live run results in evidence files, not timeless
  normative prose.

## Security reports

Do not submit secrets, private memory databases, or working exploits in a
public issue. Follow [SECURITY.md](SECURITY.md).

## Release state

No binary or Homebrew release is published. Do not present the installer or a
package-manager command as available until a tagged release and checksums have
been verified.
