package integration

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kannachi323/misty/server/api"
	"github.com/kannachi323/misty/server/billing"
	"github.com/kannachi323/misty/server/db"
	"github.com/kannachi323/misty/server/telemetry"
)

type capturedTelemetry struct{ registrations, starts, cancellations int }

func (client *capturedTelemetry) UserRegistered(string, string, string) { client.registrations++ }
func (client *capturedTelemetry) SubscriptionStarted(string, telemetry.SubscriptionProperties) {
	client.starts++
}
func (client *capturedTelemetry) SubscriptionRenewed(string, telemetry.SubscriptionProperties) {}
func (client *capturedTelemetry) SubscriptionCanceled(string, telemetry.SubscriptionProperties) {
	client.cancellations++
}
func (client *capturedTelemetry) Close(context.Context) {}

func TestRegistrationTelemetryRequiresExplicitConsent(t *testing.T) {
	database := openIntegrationDatabase(t)
	client := &capturedTelemetry{}
	handler := api.RegisterWithTelemetry(database, client)

	request := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(`{"name":"Telemetry User","username":"telemetry_`+strings.ReplaceAll(uuid.NewString(), "-", "")[:12]+`","email":"telemetry-`+uuid.NewString()+`@example.com","password":"password123"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || client.registrations != 0 {
		t.Fatalf("without consent status=%d registrations=%d", recorder.Code, client.registrations)
	}

	request = httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString(`{"name":"Telemetry User","username":"telemetry_`+strings.ReplaceAll(uuid.NewString(), "-", "")[:12]+`","email":"telemetry-`+uuid.NewString()+`@example.com","password":"password123"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Misty-Analytics-Enabled", "true")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || client.registrations != 1 {
		t.Fatalf("with consent status=%d registrations=%d", recorder.Code, client.registrations)
	}
}

func TestStripeTelemetryIsConsentAwareAndDeduplicated(t *testing.T) {
	database := openIntegrationDatabase(t)
	loadTestEnv()
	secret := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))
	if secret == "" {
		t.Fatal("missing STRIPE_WEBHOOK_SECRET")
	}
	client := &capturedTelemetry{}
	service := billing.NewStripeService(database,
		billing.WithChargeIDFetcher(func(paymentIntentID string) (string, error) { return "ch_" + paymentIntentID, nil }),
		billing.WithTelemetry(client),
	)
	handler := api.StripeWebhookWithService(secret, service)
	user, err := database.CreateUser("Telemetry Billing", "billing-"+uuid.NewString()+"@example.com", "password123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := database.UpdateTelemetryPreferences(user.ID, true, false); err != nil {
		t.Fatalf("enable analytics: %v", err)
	}

	sessionID, paymentIntentID := "cs_"+uuid.NewString(), "pi_"+uuid.NewString()
	event := checkoutEventObject(user, db.TierPersonal, sessionID, paymentIntentID, "cus_"+uuid.NewString())
	for index := 0; index < 2; index++ {
		if recorder := sendSignedStripeWebhook(t, handler, secret, "checkout.session.completed", event); recorder.Code != http.StatusOK {
			t.Fatalf("webhook %d status=%d", index, recorder.Code)
		}
	}
	if client.starts != 1 {
		t.Fatalf("subscription starts=%d, want 1", client.starts)
	}
}
