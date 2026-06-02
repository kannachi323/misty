package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kannachi323/misty/server/api"
	"github.com/kannachi323/misty/server/db"
	stripewebhook "github.com/stripe/stripe-go/v82/webhook"
)

func TestStripeWebhookCheckoutSessionCompletedUpgradesPaidTiers(t *testing.T) {
	database := openIntegrationDatabase(t)
	loadTestEnv()
	secret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if secret == "" {
		t.Fatal("missing STRIPE_WEBHOOK_SECRET")
	}

	handler := api.StripeWebhookWithService(secret, newTestStripeService(database))

	tests := []struct {
		name      string
		tier      db.Tier
		seedTrial bool
	}{
		{name: "personal_from_basic", tier: db.TierPersonal},
		{name: "pro_from_trialing_personal", tier: db.TierPro, seedTrial: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetIntegrationDatabase(t, database)

			user, err := database.CreateUser("Test User", tt.name+"@example.com", "password123")
			if err != nil {
				t.Fatalf("CreateUser() error = %v", err)
			}

			if tt.seedTrial {
				started, err := database.StartTrialByUserID(user.ID, 14*24*time.Hour)
				if err != nil {
					t.Fatalf("StartTrialByUserID() error = %v", err)
				}
				if !started {
					t.Fatal("expected personal trial to start")
				}
			}

			before, err := database.GetLicenseByUserID(user.ID)
			if err != nil || before == nil {
				t.Fatalf("GetLicenseByUserID() before webhook error = %v, license = %#v", err, before)
			}
			if tt.seedTrial && before.ExpiresAt == nil {
				t.Fatal("expected seeded trial to have expires_at")
			}

			sessionID := "cs_" + uuid.NewString()
			paymentIntentID := "pi_" + uuid.NewString()
			rec := sendSignedStripeWebhook(t, handler, secret, "checkout.session.completed", checkoutEventObject(user, tt.tier, sessionID, paymentIntentID, "cus_"+uuid.NewString()))
			if rec.Code != http.StatusOK {
				t.Fatalf("webhook status = %d, want %d, body = %q", rec.Code, http.StatusOK, rec.Body.String())
			}

			license, err := database.GetLicenseByUserID(user.ID)
			if err != nil || license == nil {
				t.Fatalf("GetLicenseByUserID() after webhook error = %v, license = %#v", err, license)
			}
			if license.Tier != tt.tier {
				t.Fatalf("license tier = %q, want %q", license.Tier, tt.tier)
			}
			if license.Status != db.LicenseStatusActive {
				t.Fatalf("license status = %q, want %q", license.Status, db.LicenseStatusActive)
			}
			if license.ExpiresAt != nil {
				t.Fatalf("license expires_at = %v, want nil", license.ExpiresAt)
			}

			purchase, err := database.GetStripePurchaseByPaymentIntent(paymentIntentID)
			if err != nil || purchase == nil {
				t.Fatalf("GetStripePurchaseByPaymentIntent() error = %v, purchase = %#v", err, purchase)
			}
			if purchase.UserID != user.ID || purchase.LicenseID != user.LicenseID {
				t.Fatalf("purchase user/license = (%q, %q), want (%q, %q)", purchase.UserID, purchase.LicenseID, user.ID, user.LicenseID)
			}
			if purchase.TierPurchased != tt.tier {
				t.Fatalf("purchase tier = %q, want %q", purchase.TierPurchased, tt.tier)
			}
			if purchase.StripeCheckoutSessionID != sessionID {
				t.Fatalf("purchase session = %q, want %q", purchase.StripeCheckoutSessionID, sessionID)
			}
			if purchase.StripeChargeID != "ch_"+paymentIntentID {
				t.Fatalf("purchase charge = %q, want %q", purchase.StripeChargeID, "ch_"+paymentIntentID)
			}
		})
	}
}

func TestStripeWebhookChargeRefundedDowngradesPaidTiers(t *testing.T) {
	runStripePurchaseLifecycleTest(t, "charge.refunded", "refunded", func(chargeID, paymentIntentID string) map[string]any {
		return map[string]any{
			"id":             chargeID,
			"payment_intent": paymentIntentID,
		}
	})
}

func TestStripeWebhookChargeDisputeCreatedDowngradesPaidTiers(t *testing.T) {
	runStripePurchaseLifecycleTest(t, "charge.dispute.created", "disputed", func(chargeID, _ string) map[string]any {
		return map[string]any{
			"id":     "dp_" + uuid.NewString(),
			"charge": chargeID,
		}
	})
}

func TestStripeWebhookRejectsInvalidSignature(t *testing.T) {
	database := openIntegrationDatabase(t)
	loadTestEnv()
	secret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if secret == "" {
		t.Fatal("missing STRIPE_WEBHOOK_SECRET")
	}

	handler := api.StripeWebhookWithService(secret, newTestStripeService(database))

	user, err := database.CreateUser("Bad Sig", "bad-sig@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	rec := sendSignedStripeWebhook(t, handler, "whsec_wrong_secret", "checkout.session.completed", checkoutEventObject(user, db.TierPersonal, "cs_"+uuid.NewString(), "pi_"+uuid.NewString(), "cus_"+uuid.NewString()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("webhook status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	license, err := database.GetLicenseByUserID(user.ID)
	if err != nil || license == nil {
		t.Fatalf("GetLicenseByUserID() error = %v, license = %#v", err, license)
	}
	if license.Tier != db.TierBasic || license.Status != db.LicenseStatusActive {
		t.Fatalf("license = (%q, %q), want (%q, %q)", license.Tier, license.Status, db.TierBasic, db.LicenseStatusActive)
	}
	if totalStripePurchases(t, database) != 0 {
		t.Fatalf("expected no stripe purchases, got %d", totalStripePurchases(t, database))
	}
}

func TestStripeWebhookIgnoresCheckoutCompletedWithInvalidMetadata(t *testing.T) {
	database := openIntegrationDatabase(t)
	loadTestEnv()
	secret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if secret == "" {
		t.Fatal("missing STRIPE_WEBHOOK_SECRET")
	}

	handler := api.StripeWebhookWithService(secret, newTestStripeService(database))

	tests := []struct {
		name     string
		metadata map[string]string
	}{
		{
			name: "missing_license_id",
			metadata: map[string]string{
				"user_id": "will-be-replaced",
				"tier":    string(db.TierPersonal),
			},
		},
		{
			name: "invalid_tier",
			metadata: map[string]string{
				"user_id":    "will-be-replaced",
				"license_id": "will-be-replaced",
				"tier":       "basic",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetIntegrationDatabase(t, database)

			user, err := database.CreateUser("Metadata User", tt.name+"@example.com", "password123")
			if err != nil {
				t.Fatalf("CreateUser() error = %v", err)
			}

			metadata := map[string]string{}
			for key, value := range tt.metadata {
				metadata[key] = value
			}
			if raw, ok := metadata["user_id"]; ok && raw == "will-be-replaced" {
				metadata["user_id"] = user.ID
			}
			if raw, ok := metadata["license_id"]; ok && raw == "will-be-replaced" {
				metadata["license_id"] = user.LicenseID
			}

			sessionID := "cs_" + uuid.NewString()
			paymentIntentID := "pi_" + uuid.NewString()
			event := checkoutEventObject(user, db.TierPersonal, sessionID, paymentIntentID, "cus_"+uuid.NewString())
			event["metadata"] = metadata

			rec := sendSignedStripeWebhook(t, handler, secret, "checkout.session.completed", event)
			if rec.Code != http.StatusOK {
				t.Fatalf("webhook status = %d, want %d", rec.Code, http.StatusOK)
			}

			license, err := database.GetLicenseByUserID(user.ID)
			if err != nil || license == nil {
				t.Fatalf("GetLicenseByUserID() error = %v, license = %#v", err, license)
			}
			if license.Tier != db.TierBasic || license.Status != db.LicenseStatusActive {
				t.Fatalf("license = (%q, %q), want (%q, %q)", license.Tier, license.Status, db.TierBasic, db.LicenseStatusActive)
			}
			if countStripePurchases(t, database, sessionID) != 0 {
				t.Fatalf("expected no stripe purchase for session %q", sessionID)
			}
		})
	}
}

func TestStripeWebhookIgnoresCheckoutCompletedWithMismatchedLicenseMetadata(t *testing.T) {
	database := openIntegrationDatabase(t)
	loadTestEnv()
	secret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if secret == "" {
		t.Fatal("missing STRIPE_WEBHOOK_SECRET")
	}

	handler := api.StripeWebhookWithService(secret, newTestStripeService(database))

	userA, err := database.CreateUser("User A", "user-a@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser(userA) error = %v", err)
	}
	userB, err := database.CreateUser("User B", "user-b@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser(userB) error = %v", err)
	}

	sessionID := "cs_" + uuid.NewString()
	paymentIntentID := "pi_" + uuid.NewString()
	event := checkoutEventObject(userA, db.TierPersonal, sessionID, paymentIntentID, "cus_"+uuid.NewString())
	event["metadata"] = map[string]string{
		"user_id":    userA.ID,
		"license_id": userB.LicenseID,
		"tier":       string(db.TierPersonal),
	}

	rec := sendSignedStripeWebhook(t, handler, secret, "checkout.session.completed", event)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook status = %d, want %d", rec.Code, http.StatusOK)
	}

	license, err := database.GetLicenseByUserID(userA.ID)
	if err != nil || license == nil {
		t.Fatalf("GetLicenseByUserID() error = %v, license = %#v", err, license)
	}
	if license.Tier != db.TierBasic || license.Status != db.LicenseStatusActive {
		t.Fatalf("license = (%q, %q), want (%q, %q)", license.Tier, license.Status, db.TierBasic, db.LicenseStatusActive)
	}
	if countStripePurchases(t, database, sessionID) != 0 {
		t.Fatalf("expected no stripe purchase for session %q", sessionID)
	}
}

func TestStripeWebhookCheckoutCompletedIsIdempotent(t *testing.T) {
	database := openIntegrationDatabase(t)
	loadTestEnv()
	secret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if secret == "" {
		t.Fatal("missing STRIPE_WEBHOOK_SECRET")
	}

	handler := api.StripeWebhookWithService(secret, newTestStripeService(database))

	user, err := database.CreateUser("Replay User", "replay@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	sessionID := "cs_" + uuid.NewString()
	paymentIntentID := "pi_" + uuid.NewString()
	event := checkoutEventObject(user, db.TierPersonal, sessionID, paymentIntentID, "cus_"+uuid.NewString())

	for i := 0; i < 2; i++ {
		rec := sendSignedStripeWebhook(t, handler, secret, "checkout.session.completed", event)
		if rec.Code != http.StatusOK {
			t.Fatalf("webhook replay %d status = %d, want %d", i+1, rec.Code, http.StatusOK)
		}
	}

	if countStripePurchases(t, database, sessionID) != 1 {
		t.Fatalf("expected one stripe purchase after replay, got %d", countStripePurchases(t, database, sessionID))
	}

	license, err := database.GetLicenseByUserID(user.ID)
	if err != nil || license == nil {
		t.Fatalf("GetLicenseByUserID() error = %v, license = %#v", err, license)
	}
	if license.Tier != db.TierPersonal || license.Status != db.LicenseStatusActive {
		t.Fatalf("license = (%q, %q), want (%q, %q)", license.Tier, license.Status, db.TierPersonal, db.LicenseStatusActive)
	}
}

func runStripePurchaseLifecycleTest(t *testing.T, eventType string, expectedStatus string, eventObject func(chargeID string, paymentIntentID string) map[string]any) {
	t.Helper()

	database := openIntegrationDatabase(t)
	loadTestEnv()
	secret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if secret == "" {
		t.Fatal("missing STRIPE_WEBHOOK_SECRET")
	}

	handler := api.StripeWebhookWithService(secret, newTestStripeService(database))

	for _, tier := range []db.Tier{db.TierPersonal, db.TierPro} {
		t.Run(string(tier), func(t *testing.T) {
			resetIntegrationDatabase(t, database)

			user, err := database.CreateUser("Lifecycle User", fmt.Sprintf("%s-%s@example.com", eventType, tier), "password123")
			if err != nil {
				t.Fatalf("CreateUser() error = %v", err)
			}

			sessionID := "cs_" + uuid.NewString()
			paymentIntentID := "pi_" + uuid.NewString()
			chargeID := "ch_" + paymentIntentID

			purchaseRec := sendSignedStripeWebhook(t, handler, secret, "checkout.session.completed", checkoutEventObject(user, tier, sessionID, paymentIntentID, "cus_"+uuid.NewString()))
			if purchaseRec.Code != http.StatusOK {
				t.Fatalf("purchase webhook status = %d, want %d", purchaseRec.Code, http.StatusOK)
			}

			rec := sendSignedStripeWebhook(t, handler, secret, eventType, eventObject(chargeID, paymentIntentID))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s webhook status = %d, want %d", eventType, rec.Code, http.StatusOK)
			}

			license, err := database.GetLicenseByUserID(user.ID)
			if err != nil || license == nil {
				t.Fatalf("GetLicenseByUserID() error = %v, license = %#v", err, license)
			}
			if license.Tier != db.TierBasic || license.Status != db.LicenseStatusActive {
				t.Fatalf("license = (%q, %q), want (%q, %q)", license.Tier, license.Status, db.TierBasic, db.LicenseStatusActive)
			}
			if license.ExpiresAt != nil {
				t.Fatalf("license expires_at = %v, want nil", license.ExpiresAt)
			}

			purchase, err := database.GetStripePurchaseByPaymentIntent(paymentIntentID)
			if err != nil || purchase == nil {
				t.Fatalf("GetStripePurchaseByPaymentIntent() error = %v, purchase = %#v", err, purchase)
			}
			if purchase.Status != expectedStatus {
				t.Fatalf("purchase status = %q, want %q", purchase.Status, expectedStatus)
			}
			if purchase.EventSource != eventType {
				t.Fatalf("purchase event source = %q, want %q", purchase.EventSource, eventType)
			}
		})
	}
}

func checkoutEventObject(user *db.User, tier db.Tier, sessionID string, paymentIntentID string, customerID string) map[string]any {
	return map[string]any{
		"id":             sessionID,
		"mode":           "payment",
		"payment_intent": paymentIntentID,
		"customer":       customerID,
		"amount_total":   int64(4900),
		"currency":       "USD",
		"metadata": map[string]string{
			"user_id":    user.ID,
			"license_id": user.LicenseID,
			"tier":       string(tier),
		},
		"customer_details": map[string]any{
			"email": user.Email,
		},
	}
}

func sendSignedStripeWebhook(t *testing.T, handler http.HandlerFunc, secret string, eventType string, object any) *httptest.ResponseRecorder {
	t.Helper()

	payload := map[string]any{
		"id":          "evt_" + uuid.NewString(),
		"object":      "event",
		"api_version": "2020-08-27",
		"type":        eventType,
		"data": map[string]any{
			"object": object,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload: body,
		Secret:  secret,
	})

	req := httptest.NewRequest(http.MethodPost, "/stripe/webhook", bytes.NewReader(signed.Payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", signed.Header)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
