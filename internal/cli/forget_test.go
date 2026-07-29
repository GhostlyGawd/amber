package cli

import (
	"testing"

	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
)

func TestRestoreTargetStatusPreservesT3Quarantine(t *testing.T) {
	tests := []struct {
		name string
		tier trust.Tier
		want string
	}{
		{name: "user stated", tier: trust.T0, want: store.StatusActive},
		{name: "user approved", tier: trust.T1, want: store.StatusActive},
		{name: "auto digest", tier: trust.T2, want: store.StatusActive},
		{name: "untrusted origin", tier: trust.T3, want: store.StatusQuarantined},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := restoreTargetStatus(&store.Memory{Trust: tt.tier})
			if got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}
