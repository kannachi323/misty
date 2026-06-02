package billing

import (
	"testing"

	"github.com/kannachi323/misty/server/db"
)

func TestTierFromMetadata(t *testing.T) {
	tests := []struct {
		raw  string
		want db.Tier
		ok   bool
	}{
		{raw: "personal", want: db.TierPersonal, ok: true},
		{raw: " Pro ", want: db.TierPro, ok: true},
		{raw: "basic", want: "", ok: false},
	}

	for _, tt := range tests {
		got, ok := tierFromMetadata(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("tierFromMetadata(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}
