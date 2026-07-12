package db

import (
	"testing"
	"time"
)

func TestCreditWalletReservationSettlementAndReset(t *testing.T) {
	database := openTestDatabase(t)
	user, err := database.CreateUser("Credit User", "credits@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	wallet, err := database.GetOrCreateCreditWallet(user.ID, TierBasic, now)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.MonthlyRemaining != 100_000 {
		t.Fatalf("basic wallet = %#v", wallet)
	}
	reservation, _, err := database.ReserveCredits(user.ID, TierBasic, CreditMeterAssistantAI, "request-1", 20_000, now)
	if err != nil {
		t.Fatal(err)
	}
	wallet, err = database.SettleCreditReservation(reservation.ID, "settle-1", CreditUsage{Provider: "openai", Model: "test", InputTokens: 10, OutputTokens: 10, Credits: 7_000})
	if err != nil {
		t.Fatal(err)
	}
	if wallet.MonthlyRemaining != 93_000 || wallet.ReservedCredits != 0 {
		t.Fatalf("settled wallet = %#v", wallet)
	}
	if err := database.AddPurchasedCredits(user.ID, "credits_1500", "purchase-1", 1_500_000); err != nil {
		t.Fatal(err)
	}
	wallet, err = database.GetOrCreateCreditWallet(user.ID, TierBasic, now)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.PurchasedRemaining != 1_500_000 {
		t.Fatalf("purchased wallet = %#v", wallet)
	}
	purchase := CreditPurchase{UserID: user.ID, StripeCheckoutSessionID: "cs_credit", StripePaymentIntentID: "pi_credit", PackID: "credits_1500", Credits: 1_500_000, Status: "completed"}
	if err := database.RecordCreditPurchase(purchase); err != nil {
		t.Fatal(err)
	}
	storedPurchase, err := database.GetCreditPurchaseByPaymentIntent("pi_credit")
	if err != nil || storedPurchase == nil {
		t.Fatalf("credit purchase = %#v, %v", storedPurchase, err)
	}
	if err := database.RefundCreditPurchase(storedPurchase); err != nil {
		t.Fatal(err)
	}
	wallet, err = database.GetOrCreateCreditWallet(user.ID, TierBasic, now)
	if err != nil || wallet.PurchasedRemaining != 0 {
		t.Fatalf("refunded wallet = %#v, %v", wallet, err)
	}
	if err := database.AddPurchasedCredits(user.ID, "credits_1500", "purchase-2", 1_500_000); err != nil {
		t.Fatal(err)
	}
	wallet, err = database.GetOrCreateCreditWallet(user.ID, TierBasic, now.AddDate(0, 2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if wallet.MonthlyRemaining != 100_000 || wallet.PurchasedRemaining != 1_500_000 {
		t.Fatalf("reset wallet = %#v", wallet)
	}
}

func TestSubscriptionFallsBackToGrandfatheredTier(t *testing.T) {
	database := openTestDatabase(t)
	user, err := database.CreateUser("Lifetime User", "lifetime@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	legacy := TierPro
	if err := database.SetLegacyTierByID(user.LicenseID, &legacy); err != nil {
		t.Fatal(err)
	}
	subscription := &StripeSubscription{UserID: user.ID, LicenseID: user.LicenseID, StripeSubscriptionID: "sub_test",
		StripeCustomerID: "cus_test", StripePriceID: "price_max", Tier: TierMax, BillingInterval: "month", Status: SubscriptionStatusActive}
	if err := database.UpsertStripeSubscription(subscription); err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyEffectiveSubscriptionEntitlement(subscription); err != nil {
		t.Fatal(err)
	}
	license, _ := database.GetLicenseByUserID(user.ID)
	if license == nil || license.Tier != TierMax {
		t.Fatalf("active subscription license = %#v", license)
	}
	subscription.Status = "canceled"
	if err := database.UpsertStripeSubscription(subscription); err != nil {
		t.Fatal(err)
	}
	if err := database.ApplyEffectiveSubscriptionEntitlement(subscription); err != nil {
		t.Fatal(err)
	}
	license, _ = database.GetLicenseByUserID(user.ID)
	if license == nil || license.Tier != TierPro {
		t.Fatalf("fallback license = %#v", license)
	}
}

func TestStartSubscriptionCreditPeriodIsIdempotent(t *testing.T) {
	database := openTestDatabase(t)
	user, err := database.CreateUser("Subscription Credit User", "subscription-credits@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	activatedAt := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	wallet, err := database.StartSubscriptionCreditPeriod(user.ID, TierPro, activatedAt, "sub_test:activation")
	if err != nil {
		t.Fatal(err)
	}
	if wallet.MonthlyAllowance != 2_000_000 || wallet.MonthlyRemaining != 2_000_000 {
		t.Fatalf("initial subscription wallet = %#v", wallet)
	}

	reservation, _, err := database.ReserveCredits(user.ID, TierPro, CreditMeterAssistantAI, "subscription-request", 20_000, activatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SettleCreditReservation(reservation.ID, "subscription-settlement", CreditUsage{Credits: 20_000}); err != nil {
		t.Fatal(err)
	}

	wallet, err = database.StartSubscriptionCreditPeriod(user.ID, TierPro, activatedAt.Add(time.Hour), "sub_test:activation")
	if err != nil {
		t.Fatal(err)
	}
	if wallet.MonthlyRemaining != 1_980_000 {
		t.Fatalf("replayed subscription grant refilled wallet: %#v", wallet)
	}
	if !wallet.AllowanceResetAt.Equal(activatedAt.AddDate(0, 1, 0)) {
		t.Fatalf("replayed subscription grant changed reset date: %s", wallet.AllowanceResetAt)
	}
}
