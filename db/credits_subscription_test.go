package db

import (
	"errors"
	"testing"
	"time"
)

func TestHostedAIWalletReservationSettlementAndWeeklyReset(t *testing.T) {
	database := openTestDatabase(t)
	user, err := database.CreateUser("Hosted AI User", "hosted-ai@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	wallet, err := database.GetOrCreateHostedAIWallet(user.ID, TierBasic, now)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.WeeklyAllowanceMicrousd != FreeWeeklyHostedAIAllowance || wallet.WeeklyRemainingMicrousd != FreeWeeklyHostedAIAllowance {
		t.Fatalf("free wallet = %#v", wallet)
	}
	reservation, wallet, err := database.ReserveHostedAIUsage(user.ID, TierBasic, HostedAIMeterAssistant, "request-1", 20_000, now)
	if err != nil || wallet.ReservedMicrousd != 20_000 {
		t.Fatalf("reservation = %#v, wallet = %#v, err = %v", reservation, wallet, err)
	}
	replayed, replayWallet, err := database.ReserveHostedAIUsage(user.ID, TierBasic, HostedAIMeterAssistant, "request-1", 20_000, now)
	if err != nil || replayed.ID != reservation.ID || replayWallet.ReservedMicrousd != 20_000 {
		t.Fatalf("idempotent reservation = %#v, wallet = %#v, err = %v", replayed, replayWallet, err)
	}
	wallet, err = database.SettleHostedAIReservation(reservation.ID, "settle-1", HostedAIUsage{Provider: "test", Model: "automatic", ChargeMicrousd: 7_000})
	if err != nil {
		t.Fatal(err)
	}
	if wallet.WeeklyRemainingMicrousd != FreeWeeklyHostedAIAllowance-7_000 || wallet.ReservedMicrousd != 0 {
		t.Fatalf("settled wallet = %#v", wallet)
	}
	replayedWallet, err := database.SettleHostedAIReservation(reservation.ID, "settle-1", HostedAIUsage{Provider: "test", Model: "automatic", ChargeMicrousd: 7_000})
	if err != nil || replayedWallet.WeeklyRemainingMicrousd != wallet.WeeklyRemainingMicrousd {
		t.Fatalf("idempotent settlement = %#v, %v", replayedWallet, err)
	}

	resetAt := wallet.ResetAt
	wallet, err = database.GetOrCreateHostedAIWallet(user.ID, TierBasic, resetAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if wallet.WeeklyRemainingMicrousd != FreeWeeklyHostedAIAllowance || !wallet.ResetAt.After(resetAt) {
		t.Fatalf("weekly reset wallet = %#v", wallet)
	}
}

func TestHostedAIPlanChangePreservesConsumedUsage(t *testing.T) {
	database := openTestDatabase(t)
	user, err := database.CreateUser("Plan Change User", "plan-change@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	reservation, _, err := database.ReserveHostedAIUsage(user.ID, TierBasic, HostedAIMeterAssistant, "request", 20_000, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.SettleHostedAIReservation(reservation.ID, "settle", HostedAIUsage{ChargeMicrousd: 20_000}); err != nil {
		t.Fatal(err)
	}
	wallet, err := database.GetOrCreateHostedAIWallet(user.ID, TierPro, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if wallet.WeeklyAllowanceMicrousd != ProWeeklyHostedAIAllowance || wallet.WeeklyRemainingMicrousd != ProWeeklyHostedAIAllowance-20_000 {
		t.Fatalf("upgrade restored consumed usage: %#v", wallet)
	}
	wallet, err = database.GetOrCreateHostedAIWallet(user.ID, TierBasic, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if wallet.WeeklyRemainingMicrousd != FreeWeeklyHostedAIAllowance-20_000 {
		t.Fatalf("downgrade changed consumed usage: %#v", wallet)
	}
}

func TestHostedAILimitAndRetiredPurchases(t *testing.T) {
	database := openTestDatabase(t)
	user, err := database.CreateUser("Limit User", "limit@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = database.ReserveHostedAIUsage(user.ID, TierBasic, HostedAIMeterAssistant, "too-large", FreeWeeklyHostedAIAllowance+1, time.Now())
	var limit HostedAILimitReachedError
	if !errors.As(err, &limit) {
		t.Fatalf("limit error = %v", err)
	}
	if err := database.AddPurchasedCredits(user.ID, "retired", "purchase", 1); !errors.Is(err, ErrCreditPurchasesRetired) {
		t.Fatalf("retired purchase error = %v", err)
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
	if license == nil || license.Tier != TierPro {
		t.Fatalf("legacy Max subscription was not normalized: %#v", license)
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
