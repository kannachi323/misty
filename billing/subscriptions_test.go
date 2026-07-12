package billing

import (
	"testing"
	"time"

	"github.com/kannachi323/misty/server/db"
)

func setStripeConfig(t *testing.T) {
	t.Helper()
	values := map[string]string{"STRIPE_SECRET_KEY": "sk_test", "STRIPE_CHECKOUT_SUCCESS_URL": "https://app/success", "STRIPE_CHECKOUT_CANCEL_URL": "https://app/cancel",
		"STRIPE_PORTAL_RETURN_URL": "https://app/account", "STRIPE_PRICE_PRO_MONTHLY": "price_pm", "STRIPE_PRICE_PRO_YEARLY": "price_py",
		"STRIPE_PRICE_MAX_MONTHLY": "price_mm", "STRIPE_PRICE_MAX_YEARLY": "price_my", "STRIPE_PRICE_CREDITS_1500": "price_c1", "STRIPE_PRICE_CREDITS_3500": "price_c2"}
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func TestLoadStripeCheckoutConfig(t *testing.T) {
	setStripeConfig(t)
	cfg, err := loadStripeCheckoutConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.prices[priceKey{db.TierMax, BillingIntervalYear}]; got != "price_my" {
		t.Fatalf("max yearly price = %q", got)
	}
}

func TestPricingConstants(t *testing.T) {
	if !validPaidTier(db.TierPro) || !validPaidTier(db.TierMax) || validPaidTier(db.TierBasic) {
		t.Fatal("paid tier validation mismatch")
	}
	if packCredits(CreditPackSmall) != 1_500_000 || packCredits(CreditPackLarge) != 3_500_000 {
		t.Fatal("credit pack mismatch")
	}
	if packAmountMinor(CreditPackSmall) != 499 || packAmountMinor(CreditPackLarge) != 999 {
		t.Fatal("credit pack amount mismatch")
	}
	if ProTrialDuration != 14*24*time.Hour {
		t.Fatal("trial duration mismatch")
	}
}
