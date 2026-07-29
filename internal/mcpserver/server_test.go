package mcpserver

import (
	"testing"

	"github.com/ghostlygawd/amber/internal/trust"
)

func TestClassifyOriginFailsClosed(t *testing.T) {
	tests := []struct {
		name       string
		origin     string
		wantTier   trust.Tier
		quarantine bool
	}{
		{name: "omitted", origin: "", wantTier: trust.T3, quarantine: true},
		{name: "dialogue", origin: "dialogue", wantTier: trust.T2, quarantine: true},
		{name: "user stated", origin: "user_stated", wantTier: trust.T0, quarantine: false},
		{name: "tool output", origin: "tool_output", wantTier: trust.T3, quarantine: true},
		{name: "web", origin: "web", wantTier: trust.T3, quarantine: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier, quarantine, reason, err := classifyOrigin(tt.origin)
			if err != nil {
				t.Fatal(err)
			}
			if tier != tt.wantTier || quarantine != tt.quarantine {
				t.Fatalf("got tier=%s quarantine=%v, want tier=%s quarantine=%v",
					tier, quarantine, tt.wantTier, tt.quarantine)
			}
			if quarantine && reason == "" {
				t.Fatal("quarantined origin must record a reason")
			}
		})
	}
}

func TestClassifyOriginRejectsUnknownValue(t *testing.T) {
	if _, _, _, err := classifyOrigin("assistant_guess"); err == nil {
		t.Fatal("expected invalid origin to fail")
	}
}

func TestClassifyOriginDoesNotTreatDialogueAsUserStated(t *testing.T) {
	tier, quarantine, _, err := classifyOrigin("dialogue")
	if err != nil {
		t.Fatal(err)
	}
	if tier == trust.T0 || !quarantine {
		t.Fatalf("dialogue got tier=%s quarantine=%v; want reviewed non-T0 memory", tier, quarantine)
	}
}
