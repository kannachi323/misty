package billing

import (
	"testing"
	"time"

	"github.com/kannachi323/misty/server/db"
)

func setStripeConfig(t *testing.T) {
	t.Helper()
	values := map[string]string{"STRIPE_SECRET_KEY": "sk_test", "STRIPE_CHECKOUT_SUCCESS_URL": "https://app/success", "STRIPE_CHECKOUT_CANCEL_URL": "https://app/cancel",
		"STRIPE_PORTAL_RETURN_URL": "https://app/account", "STRIPE_PRICE_PRO_MONTHLY": "price_pm", "STRIPE_PRICE_PRO_YEARLY": "price_py"}
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
	if got := cfg.prices[priceKey{db.TierPro, BillingIntervalYear}]; got != "price_py" {
		t.Fatalf("pro yearly price = %q", got)
	}
}

func TestPricingConstants(t *testing.T) {
	if !validPaidTier(db.TierPro) || validPaidTier(db.TierMax) || validPaidTier(db.TierBasic) {
		t.Fatal("paid tier validation mismatch")
	}
	if ProTrialDuration != 14*24*time.Hour {
		t.Fatal("trial duration mismatch")
	}
}

func TestProCheckoutParamsRequireCardForOneTimeTrial(t *testing.T) {
	setStripeConfig(t)
	cfg, err := loadStripeCheckoutConfig()
	if err != nil {
		t.Fatal(err)
	}
	user := &db.User{ID: "user_test", LicenseID: "license_test", Email: "trial@example.com"}
	for _, interval := range []BillingInterval{BillingIntervalMonth, BillingIntervalYear} {
		params := stripeCheckoutSessionParams(cfg, user, db.TierPro, interval, "", true)
		if params.SubscriptionData == nil || params.SubscriptionData.TrialPeriodDays == nil || *params.SubscriptionData.TrialPeriodDays != 14 {
			t.Fatalf("%s trial params = %#v", interval, params.SubscriptionData)
		}
		if params.PaymentMethodCollection == nil || *params.PaymentMethodCollection != "always" {
			t.Fatalf("%s payment method collection = %#v", interval, params.PaymentMethodCollection)
		}
		if len(params.LineItems) != 1 || params.LineItems[0].Price == nil || *params.LineItems[0].Price != cfg.prices[priceKey{db.TierPro, interval}] {
			t.Fatalf("%s price params = %#v", interval, params.LineItems)
		}
	}

	returning := stripeCheckoutSessionParams(cfg, user, db.TierPro, BillingIntervalMonth, "cus_existing", false)
	if returning.SubscriptionData.TrialPeriodDays != nil || returning.PaymentMethodCollection != nil {
		t.Fatalf("returning checkout incorrectly received a trial: %#v", returning)
	}
	if returning.Customer == nil || *returning.Customer != "cus_existing" {
		t.Fatalf("returning customer = %#v", returning.Customer)
	}
}
