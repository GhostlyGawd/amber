// Package version holds build identity, injected at release time via ldflags.
package version

var (
	// Version is the semantic version of this build (set by goreleaser).
	Version = "0.1.0-dev"
	// Commit is the short git SHA of this build.
	Commit = "unknown"
	// Date is the build date (RFC3339).
	Date = "unknown"
)

// SchemaVersion is the current SQLite schema version this binary writes.
const SchemaVersion = 1

// InterchangeSchema is the identifier of the export record format.
const InterchangeSchema = "amber.v1"
