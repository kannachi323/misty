package billing

import (
	"testing"

	"github.com/kannachi323/misty/server/db"
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
