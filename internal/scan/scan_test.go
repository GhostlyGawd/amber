package scan

import (
	"strings"
	"testing"
)

const (
	testGitHubToken = "ghp_0123456789abcdefghijABCDEFGHIJ0123456789"
	testAWSKey      = "AKIA0123456789ABCDEF"
)

func TestScanOrdersFindingsByPosition(t *testing.T) {
	content := "github=" + testGitHubToken + " aws=" + testAWSKey
	findings := Scan(content)
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2: %#v", len(findings), findings)
	}
	if findings[0].Start >= findings[1].Start {
		t.Fatalf("findings are not source ordered: %#v", findings)
	}
}

func TestRedactMixedSecretsRegardlessOfRuleOrder(t *testing.T) {
	content := "github=" + testGitHubToken + " aws=" + testAWSKey
	got := Redact(content, Scan(content))
	for _, secret := range []string{testGitHubToken, testAWSKey} {
		if strings.Contains(got, secret) {
			t.Fatalf("redaction left secret %q in %q", secret, got)
		}
	}
	if strings.Count(got, "[redacted:") != 2 {
		t.Fatalf("got %q, want two redaction markers", got)
	}
}

func TestRedactMergesOverlappingFindings(t *testing.T) {
	content := "token=" + testGitHubToken
	findings := Scan(content)
	if len(findings) < 2 {
		t.Fatalf("test requires overlapping token findings, got %#v", findings)
	}
	got := Redact(content, findings)
	if strings.Contains(got, testGitHubToken) {
		t.Fatalf("redaction left token in %q", got)
	}
	if !strings.Contains(got, "[redacted:") {
		t.Fatalf("got %q, want a redaction marker", got)
	}
}

func TestRedactAllMergesSecretAndPIIOverlap(t *testing.T) {
	content := "contact test@example.com"
	findings := []Finding{
		{Class: ClassPII, Kind: "email", Start: 8, End: len(content)},
		{Class: ClassSecret, Kind: "assigned-credential", Start: 0, End: len(content)},
	}
	got := RedactAll(content, findings)
	if strings.Contains(got, "test@example.com") {
		t.Fatalf("redaction left PII in %q", got)
	}
}
