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
		{raw: "personal", want: db.TierPro, ok: true},
		{raw: " Pro ", want: db.TierPro, ok: true},
		{raw: "max", want: db.TierMax, ok: true},
		{raw: "basic", want: "", ok: false},
	}

	for _, tt := range tests {
		got, ok := tierFromMetadata(tt.raw)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("tierFromMetadata(%q) = (%q, %v), want (%q, %v)", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSubscriptionStartTelemetryDeduplicatesCompletedCheckout(t *testing.T) {
	if !shouldCaptureSubscriptionStart(nil) {
		t.Fatal("new checkout should be captured")
	}
	if shouldCaptureSubscriptionStart(&db.StripePurchase{Status: stripePurchaseStatusCompleted}) {
		t.Fatal("completed replay should not be captured")
	}
	if !shouldCaptureSubscriptionStart(&db.StripePurchase{Status: stripePurchaseStatusRefunded}) {
		t.Fatal("a non-completed state is not a duplicate start")
	}
}

func TestConfiguredSubscriptionPriceIsAuthoritative(t *testing.T) {
	t.Setenv("STRIPE_PRICE_MAX_MONTHLY", "price_max_month")
	tier, interval, ok := configuredSubscriptionPrice("price_max_month")
	if !ok || tier != db.TierMax || interval != BillingIntervalMonth {
		t.Fatalf("configured price = (%q, %q, %v)", tier, interval, ok)
	}
	if _, _, ok := configuredSubscriptionPrice("price_unknown"); ok {
		t.Fatal("unknown price was accepted")
	}
}
