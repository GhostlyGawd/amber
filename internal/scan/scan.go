// Package scan detects secrets and PII in memory content before it is
// written or exported (§10).
//
// Policy (config scan.mode):
//   - "warn":  findings refuse the write unless --force. With --force,
//     secrets are stored REDACTED (a leaked key is never a useful memory);
//     PII is stored as given (the user explicitly confirmed) with a warning.
//   - "block": findings always refuse the write.
//
// Digest drops flagged candidates outright. Export scans outgoing text and
// redacts secrets, then still warns the user to review before sharing
// (Codex parity).
package scan

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// Class of finding.
type Class string

const (
	ClassSecret Class = "secret"
	ClassPII    Class = "pii"
)

// Finding is one detected span.
type Finding struct {
	Class Class  `json:"class"`
	Kind  string `json:"kind"` // e.g. aws-access-key, email, jwt
	Match string `json:"match"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type rule struct {
	kind  string
	class Class
	re    *regexp.Regexp
	// verify optionally rejects a regex match (e.g. Luhn for cards).
	verify func(string) bool
}

var rules = []rule{
	// --- secrets ---
	{kind: "aws-access-key", class: ClassSecret, re: regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[0-9A-Z]{16}\b`)},
	{kind: "github-token", class: ClassSecret, re: regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,255}\b`)},
	{kind: "github-fine-grained-pat", class: ClassSecret, re: regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,255}\b`)},
	{kind: "gitlab-token", class: ClassSecret, re: regexp.MustCompile(`\bglpat-[A-Za-z0-9_\-]{20,}\b`)},
	{kind: "slack-token", class: ClassSecret, re: regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{kind: "openai-or-anthropic-key", class: ClassSecret, re: regexp.MustCompile(`\bsk-(?:ant-|proj-)?[A-Za-z0-9_\-]{20,}\b`)},
	{kind: "google-api-key", class: ClassSecret, re: regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{kind: "stripe-key", class: ClassSecret, re: regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[A-Za-z0-9]{20,}\b`)},
	{kind: "npm-token", class: ClassSecret, re: regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)},
	{kind: "private-key-block", class: ClassSecret, re: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{kind: "jwt", class: ClassSecret, re: regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\b`)},
	{kind: "assigned-credential", class: ClassSecret,
		re: regexp.MustCompile(`(?i)\b(api[_-]?key|apikey|secret|token|password|passwd|pwd|credential|auth)\b\s*[:=]\s*['"]?([A-Za-z0-9_\-/+=.]{12,})`),
		verify: func(m string) bool {
			// The captured value must not be a placeholder.
			low := strings.ToLower(m)
			for _, p := range []string{"xxxx", "your_", "<", "example", "changeme", "placeholder", "redacted", "env", "1234567890"} {
				if strings.Contains(low, p) {
					return false
				}
			}
			return true
		}},

	// --- PII ---
	{kind: "email", class: ClassPII, re: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)},
	{kind: "us-ssn", class: ClassPII, re: regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
	{kind: "phone", class: ClassPII, re: regexp.MustCompile(`(?:^|[\s(:])(\+\d{1,3}[\s.\-]?)?(\(?\d{3}\)?[\s.\-]\d{3}[\s.\-]\d{4})\b`)},
	{kind: "credit-card", class: ClassPII, re: regexp.MustCompile(`\b(?:\d[ \-]?){13,19}\b`), verify: luhnOK},
	{kind: "iban", class: ClassPII, re: regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b`), verify: ibanPlausible},
}

// entropyContext requires a nearby credential word before flagging
// high-entropy strings, keeping false positives down.
var entropyContext = regexp.MustCompile(`(?i)\b(key|secret|token|auth|bearer|credential|password|passwd)\b`)
var entropyCandidate = regexp.MustCompile(`\b[A-Za-z0-9/+_\-]{20,}\b`)

// Scan returns all findings in content, ordered by position.
func Scan(content string) []Finding {
	var out []Finding
	for _, r := range rules {
		for _, loc := range r.re.FindAllStringIndex(content, -1) {
			m := content[loc[0]:loc[1]]
			if r.verify != nil && !r.verify(m) {
				continue
			}
			out = append(out, Finding{Class: r.class, Kind: r.kind, Match: m, Start: loc[0], End: loc[1]})
		}
	}
	// Entropy pass: long random-looking tokens near credential words.
	if entropyContext.MatchString(content) {
		for _, loc := range entropyCandidate.FindAllStringIndex(content, -1) {
			m := content[loc[0]:loc[1]]
			if covered(out, loc[0], loc[1]) {
				continue
			}
			ctxStart := max(0, loc[0]-48)
			ctxEnd := min(len(content), loc[1]+16)
			if !entropyContext.MatchString(content[ctxStart:ctxEnd]) {
				continue
			}
			if shannon(m) >= 3.9 || (isHex(m) && len(m) >= 32 && shannon(m) >= 3.0) {
				out = append(out, Finding{Class: ClassSecret, Kind: "high-entropy-near-credential-word", Match: m, Start: loc[0], End: loc[1]})
			}
		}
	}
	// Dedupe overlapping findings (keep the earliest/most specific).
	out = dedupe(out)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].End < out[j].End
	})
	return out
}

// Redact replaces secret spans with [redacted:<kind>]. PII spans are left
// intact — callers decide per policy (see package doc).
func Redact(content string, findings []Finding) string {
	return redactWhere(content, findings, func(f Finding) bool { return f.Class == ClassSecret })
}

// RedactAll replaces both secret and PII spans (used for export --safe).
func RedactAll(content string, findings []Finding) string {
	return redactWhere(content, findings, func(Finding) bool { return true })
}

func redactWhere(content string, findings []Finding, want func(Finding) bool) string {
	spans := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if !want(f) || f.Start < 0 || f.End > len(content) || f.Start >= f.End {
			continue
		}
		spans = append(spans, f)
	}

	if len(spans) == 0 {
		return content
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].Start != spans[j].Start {
			return spans[i].Start < spans[j].Start
		}
		return spans[i].End > spans[j].End
	})

	// Merge overlapping spans before replacement. Findings can overlap when
	// a provider-specific token also matches the generic assigned-credential
	// rule. Replacing either span first would invalidate the other's offsets.
	merged := make([]Finding, 0, len(spans))
	for _, f := range spans {
		if len(merged) == 0 || f.Start >= merged[len(merged)-1].End {
			merged = append(merged, f)
			continue
		}
		last := &merged[len(merged)-1]
		if f.End > last.End {
			last.End = f.End
		}
		if last.Kind != f.Kind {
			last.Kind = "multiple"
		}
	}

	// Replace back-to-front so source offsets stay valid.
	for i := len(merged) - 1; i >= 0; i-- {
		f := merged[i]
		content = content[:f.Start] + "[redacted:" + f.Kind + "]" + content[f.End:]
	}
	return content
}

// Summary renders findings as a one-line report.
func Summary(findings []Finding) string {
	if len(findings) == 0 {
		return "no secrets or PII detected"
	}
	counts := map[string]int{}
	var order []string
	for _, f := range findings {
		k := string(f.Class) + ":" + f.Kind
		if counts[k] == 0 {
			order = append(order, k)
		}
		counts[k]++
	}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%s ×%d", k, counts[k]))
	}
	return strings.Join(parts, ", ")
}

// HasSecrets reports whether any finding is a secret.
func HasSecrets(fs []Finding) bool {
	for _, f := range fs {
		if f.Class == ClassSecret {
			return true
		}
	}
	return false
}

func covered(fs []Finding, start, end int) bool {
	for _, f := range fs {
		if start >= f.Start && end <= f.End {
			return true
		}
	}
	return false
}

func dedupe(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		dup := false
		for _, g := range out {
			if f.Start >= g.Start && f.End <= g.End {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, f)
		}
	}
	return out
}

func shannon(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	var freq [256]int
	for i := 0; i < len(s); i++ {
		freq[s[i]]++
	}
	var h float64
	n := float64(len(s))
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return len(s) > 0
}

func luhnOK(s string) bool {
	var digits []int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits = append(digits, int(c-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum, alt := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

var ibanCountries = map[string]bool{
	"AD": true, "AT": true, "BE": true, "CH": true, "CZ": true, "DE": true, "DK": true,
	"ES": true, "FI": true, "FR": true, "GB": true, "IE": true, "IT": true, "LI": true,
	"LU": true, "NL": true, "NO": true, "PL": true, "PT": true, "SE": true,
}

func ibanPlausible(s string) bool {
	if len(s) < 15 {
		return false
	}
	return ibanCountries[s[:2]]
}
