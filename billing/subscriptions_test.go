package billing

import (
	"testing"

	"github.com/kannachi323/misty/server/db"
	"github.com/stripe/stripe-go/v82"
)

func TestLoadStripeCheckoutConfig(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_CHECKOUT_SUCCESS_URL", "https://app.example.com/success")
	t.Setenv("STRIPE_CHECKOUT_CANCEL_URL", "https://app.example.com/cancel")
	t.Setenv("STRIPE_PRICE_PERSONAL", "price_personal")
	t.Setenv("STRIPE_PRICE_PRO", "price_pro")

	cfg, err := loadStripeCheckoutConfig()
	if err != nil {
		t.Fatalf("loadStripeCheckoutConfig() error = %v", err)
	}

	if cfg.prices[db.TierPersonal] != "price_personal" {
		t.Fatalf("personal price = %q, want %q", cfg.prices[db.TierPersonal], "price_personal")
	}
	if cfg.prices[db.TierPro] != "price_pro" {
		t.Fatalf("pro price = %q, want %q", cfg.prices[db.TierPro], "price_pro")
	}
}

func TestLoadStripeCheckoutConfigRequiresPrice(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_123")
	t.Setenv("STRIPE_CHECKOUT_SUCCESS_URL", "https://app.example.com/success")
	t.Setenv("STRIPE_CHECKOUT_CANCEL_URL", "https://app.example.com/cancel")
	t.Setenv("STRIPE_PRICE_PERSONAL", "price_personal")
	t.Setenv("STRIPE_PRICE_PRO", "")

	if _, err := loadStripeCheckoutConfig(); err == nil {
		t.Fatal("loadStripeCheckoutConfig() succeeded without STRIPE_PRICE_PRO")
	}
}

func TestShouldApplyProUpgradeDiscount(t *testing.T) {
	license := &db.License{Tier: db.TierPersonal, Status: db.LicenseStatusActive}
	if !shouldApplyProUpgradeDiscount(license, true, db.TierPro) {
		t.Fatal("expected active personal purchase to be upgrade eligible")
	}
	if shouldApplyProUpgradeDiscount(license, false, db.TierPro) {
		t.Fatal("expected missing personal purchase to block upgrade discount")
	}
	if shouldApplyProUpgradeDiscount(&db.License{Tier: db.TierPersonal, Status: db.LicenseStatusTrialing}, true, db.TierPro) {
		t.Fatal("expected trialing personal license to block upgrade discount")
	}
	if shouldApplyProUpgradeDiscount(&db.License{Tier: db.TierBasic, Status: db.LicenseStatusActive}, true, db.TierPro) {
		t.Fatal("expected basic license to block upgrade discount")
	}
	if shouldApplyProUpgradeDiscount(license, true, db.TierPersonal) {
		t.Fatal("expected personal checkout to skip upgrade discount")
	}
}

func TestComputeUpgradeDiscount(t *testing.T) {
	personal := &stripe.Price{Currency: stripe.CurrencyUSD, UnitAmount: 2500}

	amountOff, currency, err := computeUpgradeDiscount(personal)
	if err != nil {
		t.Fatalf("computeUpgradeDiscount() error = %v", err)
	}
	if amountOff != 2500 {
		t.Fatalf("amountOff = %d, want %d", amountOff, 2500)
	}
	if currency != "usd" {
		t.Fatalf("currency = %q, want %q", currency, "usd")
	}
}

func TestComputeUpgradeDiscountRejectsInvalidPricing(t *testing.T) {
	tests := []struct {
		name     string
		personal *stripe.Price
	}{
		{
			name:     "missing price",
			personal: nil,
		},
		{
			name:     "missing amount",
			personal: &stripe.Price{Currency: stripe.CurrencyUSD, UnitAmount: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := computeUpgradeDiscount(tt.personal); err == nil {
				t.Fatal("computeUpgradeDiscount() succeeded for invalid pricing")
			}
		})
	}
}
