package billing

import (
	"encoding/json"
	"log"
	"os"
	"strings"

	"github.com/kannachi323/misty/server/db"
	"github.com/stripe/stripe-go/v82"
	paymentintentapi "github.com/stripe/stripe-go/v82/paymentintent"
)

const (
	stripePurchaseStatusCompleted = "completed"
	stripePurchaseStatusRefunded  = "refunded"
	stripePurchaseStatusDisputed  = "disputed"
)

type checkoutCompletedEvent struct {
	ID              string            `json:"id"`
	Mode            string            `json:"mode"`
	Metadata        map[string]string `json:"metadata"`
	PaymentIntent   string            `json:"payment_intent"`
	Customer        string            `json:"customer"`
	AmountTotal     int64             `json:"amount_total"`
	Currency        string            `json:"currency"`
	CustomerDetails struct {
		Email string `json:"email"`
	} `json:"customer_details"`
}

type refundedChargeEvent struct {
	ID            string `json:"id"`
	PaymentIntent string `json:"payment_intent"`
}

type disputeEvent struct {
	ID     string `json:"id"`
	Charge string `json:"charge"`
}

type ChargeIDFetcher func(paymentIntentID string) (string, error)

type StripeService struct {
	database      *db.Database
	fetchChargeID ChargeIDFetcher
}

type StripeOption func(*StripeService)

func NewStripeService(database *db.Database, opts ...StripeOption) *StripeService {
	service := &StripeService{
		database:      database,
		fetchChargeID: fetchChargeIDFromStripe,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func WithChargeIDFetcher(fn ChargeIDFetcher) StripeOption {
	return func(service *StripeService) {
		if fn != nil {
			service.fetchChargeID = fn
		}
	}
}

func (service *StripeService) HandleWebhookEvent(eventType string, payload json.RawMessage) {
	switch eventType {
	case "checkout.session.completed":
		var session checkoutCompletedEvent
		if err := json.Unmarshal(payload, &session); err != nil {
			log.Println("Failed to parse checkout session:", err)
			return
		}
		service.handleCheckoutCompleted(&session)
	case "charge.refunded":
		var charge refundedChargeEvent
		if err := json.Unmarshal(payload, &charge); err != nil {
			log.Println("Failed to parse refunded charge:", err)
			return
		}
		service.handleChargeRefunded(&charge)
	case "charge.dispute.created":
		var dispute disputeEvent
		if err := json.Unmarshal(payload, &dispute); err != nil {
			log.Println("Failed to parse dispute:", err)
			return
		}
		service.handleChargeDisputeCreated(&dispute)
	}
}

func (service *StripeService) handleCheckoutCompleted(session *checkoutCompletedEvent) {
	if session.Mode != "payment" {
		return
	}

	userID := strings.TrimSpace(session.Metadata["user_id"])
	licenseID := strings.TrimSpace(session.Metadata["license_id"])
	tier, ok := tierFromMetadata(session.Metadata["tier"])
	if userID == "" || licenseID == "" || !ok {
		log.Printf("Stripe checkout missing required metadata for session %s", session.ID)
		return
	}

	user, err := service.database.GetUserByID(userID)
	if err != nil || user == nil {
		log.Printf("Stripe checkout: user %s not found: %v", userID, err)
		return
	}
	if user.LicenseID != licenseID {
		log.Printf("Stripe checkout license mismatch for user %s: have %s want %s", userID, user.LicenseID, licenseID)
		return
	}

	if err := service.database.SetLicenseStateByID(licenseID, tier, db.LicenseStatusActive, nil); err != nil {
		log.Printf("Failed to activate %s license %s: %v", tier, licenseID, err)
		return
	}

	chargeID, err := service.fetchChargeID(session.PaymentIntent)
	if err != nil {
		log.Printf("Failed to fetch charge for payment intent %s: %v", session.PaymentIntent, err)
	}

	if err := service.database.UpsertStripePurchase(&db.StripePurchase{
		UserID:                  userID,
		LicenseID:               licenseID,
		TierPurchased:           tier,
		StripeCheckoutSessionID: session.ID,
		StripePaymentIntentID:   session.PaymentIntent,
		StripeCustomerID:        session.Customer,
		StripeChargeID:          chargeID,
		Amount:                  session.AmountTotal,
		Currency:                strings.ToLower(strings.TrimSpace(session.Currency)),
		Status:                  stripePurchaseStatusCompleted,
		EventSource:             "checkout.session.completed",
	}); err != nil {
		log.Printf("Failed to persist Stripe purchase for session %s: %v", session.ID, err)
		return
	}

	log.Printf("Provisioned %s license for user %s (%s)", tier, userID, session.CustomerDetails.Email)
}

func (service *StripeService) handleChargeRefunded(charge *refundedChargeEvent) {
	purchase, err := service.database.GetStripePurchaseByPaymentIntent(strings.TrimSpace(charge.PaymentIntent))
	if err != nil {
		log.Printf("Failed to find purchase for refunded payment intent %s: %v", charge.PaymentIntent, err)
		return
	}
	if purchase == nil {
		purchase, err = service.database.GetStripePurchaseByChargeID(strings.TrimSpace(charge.ID))
		if err != nil {
			log.Printf("Failed to find purchase for refunded charge %s: %v", charge.ID, err)
			return
		}
	}
	if purchase == nil {
		log.Printf("No purchase found for refunded charge %s", charge.ID)
		return
	}

	if err := service.database.UpdateStripePurchaseStatus(purchase.ID, stripePurchaseStatusRefunded, "charge.refunded"); err != nil {
		log.Printf("Failed to mark purchase %s refunded: %v", purchase.ID, err)
		return
	}
	if err := service.database.SetLicenseStateByID(purchase.LicenseID, db.TierBasic, db.LicenseStatusActive, nil); err != nil {
		log.Printf("Failed to downgrade license %s after refund: %v", purchase.LicenseID, err)
	}
}

func (service *StripeService) handleChargeDisputeCreated(dispute *disputeEvent) {
	purchase, err := service.database.GetStripePurchaseByChargeID(strings.TrimSpace(dispute.Charge))
	if err != nil {
		log.Printf("Failed to find purchase for disputed charge %s: %v", dispute.Charge, err)
		return
	}
	if purchase == nil {
		log.Printf("No purchase found for disputed charge %s", dispute.Charge)
		return
	}

	if err := service.database.UpdateStripePurchaseStatus(purchase.ID, stripePurchaseStatusDisputed, "charge.dispute.created"); err != nil {
		log.Printf("Failed to mark purchase %s disputed: %v", purchase.ID, err)
		return
	}
	if err := service.database.SetLicenseStateByID(purchase.LicenseID, db.TierBasic, db.LicenseStatusActive, nil); err != nil {
		log.Printf("Failed to downgrade license %s after dispute: %v", purchase.LicenseID, err)
	}
}

func tierFromMetadata(value string) (db.Tier, bool) {
	switch db.Tier(strings.ToLower(strings.TrimSpace(value))) {
	case db.TierPersonal:
		return db.TierPersonal, true
	case db.TierPro:
		return db.TierPro, true
	default:
		return "", false
	}
}

func fetchChargeIDFromStripe(paymentIntentID string) (string, error) {
	secretKey := strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY"))
	if secretKey == "" || paymentIntentID == "" {
		return "", nil
	}

	stripe.Key = secretKey
	params := &stripe.PaymentIntentParams{}
	params.AddExpand("latest_charge")

	intent, err := paymentintentapi.Get(paymentIntentID, params)
	if err != nil || intent == nil || intent.LatestCharge == nil {
		return "", err
	}

	return intent.LatestCharge.ID, nil
}
