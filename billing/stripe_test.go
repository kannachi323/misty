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
		{raw: "personal", want: "", ok: false},
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
	t.Setenv("STRIPE_PRICE_PRO_MONTHLY", "price_pro_month")
	t.Setenv("STRIPE_PRICE_PRO_YEARLY", "price_pro_year")
	t.Setenv("STRIPE_PRICE_MAX_MONTHLY", "price_max_month")
	t.Setenv("STRIPE_PRICE_MAX_YEARLY", "price_max_year")
	for priceID, want := range map[string]struct {
		tier     db.Tier
		interval BillingInterval
	}{
		"price_pro_month": {db.TierPro, BillingIntervalMonth},
		"price_pro_year":  {db.TierPro, BillingIntervalYear},
		"price_max_month": {db.TierMax, BillingIntervalMonth},
		"price_max_year":  {db.TierMax, BillingIntervalYear},
	} {
		tier, interval, ok := configuredSubscriptionPrice(priceID)
		if !ok || tier != want.tier || interval != want.interval {
			t.Fatalf("configured price %q = (%q, %q, %v)", priceID, tier, interval, ok)
		}
	}
	if _, _, ok := configuredSubscriptionPrice("price_unknown"); ok {
		t.Fatal("unknown price was accepted")
	}
}
