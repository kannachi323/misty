package db

import "testing"

func TestStripePurchaseUpsertAndLookup(t *testing.T) {
	database := openTestDatabase(t)

	user, err := database.CreateUser("Buyer", "buyer@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	purchase := &StripePurchase{
		UserID:                  user.ID,
		LicenseID:               user.LicenseID,
		TierPurchased:           TierPro,
		StripeCheckoutSessionID: "cs_test_1",
		StripePaymentIntentID:   "pi_test_1",
		StripeChargeID:          "ch_test_1",
		Status:                  "completed",
		EventSource:             "test",
	}
	if err := database.UpsertStripePurchase(purchase); err != nil {
		t.Fatalf("UpsertStripePurchase() error = %v", err)
	}

	purchase.Status = "refunded"
	purchase.EventSource = "charge.refunded"
	if err := database.UpsertStripePurchase(purchase); err != nil {
		t.Fatalf("UpsertStripePurchase() second error = %v", err)
	}

	byIntent, err := database.GetStripePurchaseByPaymentIntent("pi_test_1")
	if err != nil || byIntent == nil {
		t.Fatalf("GetStripePurchaseByPaymentIntent() error = %v, purchase = %#v", err, byIntent)
	}
	if byIntent.Status != "refunded" {
		t.Fatalf("purchase status = %q, want %q", byIntent.Status, "refunded")
	}

	byCharge, err := database.GetStripePurchaseByChargeID("ch_test_1")
	if err != nil || byCharge == nil {
		t.Fatalf("GetStripePurchaseByChargeID() error = %v, purchase = %#v", err, byCharge)
	}
	if byCharge.ID != byIntent.ID {
		t.Fatalf("purchase IDs mismatch: %q vs %q", byCharge.ID, byIntent.ID)
	}
}
