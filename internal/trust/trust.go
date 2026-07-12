// Package trust defines provenance trust tiers (decision D13).
//
// A memory's trust tier records where it came from, not how true it is.
// Tier T3 (untrusted origin) is quarantined: never auto-injected until a
// human reviews it. Review promotes to T1.
package trust

import "fmt"

// Tier is the provenance level of a memory.
type Tier int

const (
	// T0 UserStated: typed directly by the user (`amber remember`).
	T0 Tier = 0
	// T1 UserApproved: reviewed and approved by the user.
	T1 Tier = 1
	// T2 AutoDigest: extracted by the LLM from clean dialogue.
	T2 Tier = 2
	// T3 UntrustedOrigin: derived from tool output, web content, or any
	// other span an adversary could control. Quarantined on write.
	T3 Tier = 3
)

// String returns the short canonical label.
func (t Tier) String() string {
	switch t {
	case T0:
		return "T0"
	case T1:
		return "T1"
	case T2:
		return "T2"
	case T3:
		return "T3"
	}
	return fmt.Sprintf("T%d", int(t))
}

// Label returns the human description used in `show`, `browse`, and docs.
func (t Tier) Label() string {
	switch t {
	case T0:
		return "user-stated"
	case T1:
		return "user-approved"
	case T2:
		return "auto-digest"
	case T3:
		return "untrusted-origin"
	}
	return "unknown"
}

// Valid reports whether t is a defined tier.
func (t Tier) Valid() bool { return t >= T0 && t <= T3 }

// Injectable reports whether memories of this tier may be auto-injected
// into session context. T3 never is; review promotes it to T1 first.
func (t Tier) Injectable() bool { return t == T0 || t == T1 || t == T2 }

// RankFactor is the retrieval score multiplier for the tier.
// Provenance modulates ranking: user-stated beliefs outrank auto-digested
// ones when otherwise equally relevant.
func (t Tier) RankFactor() float64 {
	switch t {
	case T0:
		return 1.0
	case T1:
		return 0.97
	case T2:
		return 0.90
	case T3:
		return 0.75 // only reachable with --history
	}
	return 0.5
}

// InitialConfidence is the starting belief confidence for a new memory of
// this tier.
func (t Tier) InitialConfidence() float64 {
	switch t {
	case T0:
		return 1.0
	case T1:
		return 0.95
	case T2:
		return 0.80
	case T3:
		return 0.50
	}
	return 0.5
}
