package billing

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kannachi323/misty/server/db"
	"github.com/kannachi323/misty/server/telemetry"
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
	Subscription    string            `json:"subscription"`
	PaymentStatus   string            `json:"payment_status"`
	AmountTotal     int64             `json:"amount_total"`
	Currency        string            `json:"currency"`
	CustomerDetails struct {
		Email string `json:"email"`
	} `json:"customer_details"`
}

type subscriptionEvent struct {
	ID                string            `json:"id"`
	Customer          string            `json:"customer"`
	Status            string            `json:"status"`
	Metadata          map[string]string `json:"metadata"`
	CurrentPeriodEnd  int64             `json:"current_period_end"`
	CancelAtPeriodEnd bool              `json:"cancel_at_period_end"`
	CanceledAt        int64             `json:"canceled_at"`
	Items             struct {
		Data []struct {
			CurrentPeriodEnd int64 `json:"current_period_end"`
			Price            struct {
				ID        string `json:"id"`
				Recurring struct {
					Interval string `json:"interval"`
				} `json:"recurring"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

type invoiceEvent struct {
	ID            string `json:"id"`
	Subscription  string `json:"subscription"`
	BillingReason string `json:"billing_reason"`
	AmountPaid    int64  `json:"amount_paid"`
	Currency      string `json:"currency"`
	Parent        struct {
		SubscriptionDetails struct {
			Subscription string `json:"subscription"`
		} `json:"subscription_details"`
	} `json:"parent"`
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
	telemetry     telemetry.Client
}

type StripeOption func(*StripeService)

func NewStripeService(database *db.Database, opts ...StripeOption) *StripeService {
	service := &StripeService{
		database:      database,
		fetchChargeID: fetchChargeIDFromStripe,
		telemetry:     telemetry.NoopClient{},
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func WithTelemetry(client telemetry.Client) StripeOption {
	return func(service *StripeService) {
		if client != nil {
			service.telemetry = client
		}
	}
}

func WithChargeIDFetcher(fn ChargeIDFetcher) StripeOption {
	return func(service *StripeService) {
		if fn != nil {
			service.fetchChargeID = fn
		}
	}
}

func (service *StripeService) HandleWebhookEvent(eventType string, payload json.RawMessage) {
	if err := service.HandleWebhookEventWithID("", eventType, payload); err != nil {
		log.Printf("Stripe webhook %s failed: %v", eventType, err)
	}
}

func (service *StripeService) HandleWebhookEventWithID(eventID, eventType string, payload json.RawMessage) error {
	if eventID != "" {
		processed, err := service.database.StripeEventProcessed(eventID)
		if err != nil {
			return err
		}
		if processed {
			return nil
		}
	}
	switch eventType {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var session checkoutCompletedEvent
		if err := json.Unmarshal(payload, &session); err != nil {
			log.Println("Failed to parse checkout session:", err)
			return err
		}
		if err := service.handleCheckout(&session); err != nil {
			return err
		}
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var subscription subscriptionEvent
		if err := json.Unmarshal(payload, &subscription); err != nil {
			return err
		}
		if err := service.handleSubscriptionChanged(&subscription); err != nil {
			return err
		}
	case "invoice.paid", "invoice.payment_failed":
		var invoice invoiceEvent
		if err := json.Unmarshal(payload, &invoice); err != nil {
			return err
		}
		if eventType == "invoice.paid" {
			if err := service.handleInvoicePaid(&invoice); err != nil {
				return err
			}
		}
	case "charge.refunded":
		var charge refundedChargeEvent
		if err := json.Unmarshal(payload, &charge); err != nil {
			log.Println("Failed to parse refunded charge:", err)
			return err
		}
		service.handleChargeRefunded(&charge)
	case "charge.dispute.created":
		var dispute disputeEvent
		if err := json.Unmarshal(payload, &dispute); err != nil {
			log.Println("Failed to parse dispute:", err)
			return err
		}
		service.handleChargeDisputeCreated(&dispute)
	}
	if eventID != "" {
		return service.database.MarkStripeEventProcessed(eventID, eventType)
	}
	return nil
}

func (service *StripeService) handleCheckout(session *checkoutCompletedEvent) error {
	kind := strings.TrimSpace(session.Metadata["kind"])
	if session.Mode == "subscription" || kind == "subscription" {
		// Entitlement is applied from customer.subscription.* where Stripe supplies canonical status and period data.
		return nil
	}
	if kind == "credit_pack" {
		userID := strings.TrimSpace(session.Metadata["user_id"])
		packID := strings.TrimSpace(session.Metadata["pack_id"])
		expectedAmount := packAmountMinor(packID)
		if userID == "" || packCredits(packID) == 0 || expectedAmount == 0 {
			return errors.New("credit checkout metadata is invalid")
		}
		if !strings.EqualFold(session.PaymentStatus, "paid") || session.AmountTotal != expectedAmount || !strings.EqualFold(session.Currency, "usd") {
			return errors.New("credit checkout payment is not paid or does not match the pack")
		}
		license, err := service.database.GetLicenseByUserID(userID)
		if err != nil {
			return err
		}
		if license == nil {
			return ErrLicenseNotFound
		}
		if _, err := service.database.GetOrCreateCreditWallet(userID, license.Tier, time.Now()); err != nil {
			return err
		}
		if err := service.database.RecordCreditPurchase(db.CreditPurchase{UserID: userID, StripeCheckoutSessionID: session.ID,
			StripePaymentIntentID: session.PaymentIntent, PackID: packID, Credits: packCredits(packID), Status: "completed"}); err != nil {
			return err
		}
		return service.database.AddPurchasedCredits(userID, packID, "stripe_checkout:"+session.ID, packCredits(packID))
	}
	service.handleCheckoutCompleted(session)
	return nil
}

func (service *StripeService) handleSubscriptionChanged(event *subscriptionEvent) error {
	userID := strings.TrimSpace(event.Metadata["user_id"])
	licenseID := strings.TrimSpace(event.Metadata["license_id"])
	tier, ok := tierFromMetadata(event.Metadata["tier"])
	if userID == "" || licenseID == "" || !ok || !validPaidTier(tier) {
		return errors.New("subscription metadata is invalid")
	}
	user, err := service.database.GetUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil || user.LicenseID != licenseID {
		return errors.New("subscription license does not match user")
	}
	interval := strings.TrimSpace(event.Metadata["interval"])
	priceID := ""
	if len(event.Items.Data) > 0 {
		priceID = event.Items.Data[0].Price.ID
		if itemInterval := strings.TrimSpace(event.Items.Data[0].Price.Recurring.Interval); itemInterval != "" {
			interval = itemInterval
		}
	}
	catalogTier, catalogInterval, catalogOK := configuredSubscriptionPrice(priceID)
	if !catalogOK {
		return errors.New("subscription price is not in the configured catalog")
	}
	if catalogTier != tier {
		return errors.New("subscription tier metadata does not match the Stripe price")
	}
	tier = catalogTier
	interval = string(catalogInterval)
	var periodEnd, canceledAt *time.Time
	if event.CurrentPeriodEnd > 0 {
		value := time.Unix(event.CurrentPeriodEnd, 0).UTC()
		periodEnd = &value
	} else if len(event.Items.Data) > 0 && event.Items.Data[0].CurrentPeriodEnd > 0 {
		value := time.Unix(event.Items.Data[0].CurrentPeriodEnd, 0).UTC()
		periodEnd = &value
	}
	if event.CanceledAt > 0 {
		value := time.Unix(event.CanceledAt, 0).UTC()
		canceledAt = &value
	}
	previous, err := service.database.GetStripeSubscriptionByStripeID(event.ID)
	if err != nil {
		return err
	}
	subscription := &db.StripeSubscription{UserID: userID, LicenseID: licenseID, StripeSubscriptionID: event.ID,
		StripeCustomerID: event.Customer, StripePriceID: priceID, Tier: tier, BillingInterval: interval,
		Status: strings.ToLower(strings.TrimSpace(event.Status)), CurrentPeriodEnd: periodEnd,
		CancelAtPeriodEnd: event.CancelAtPeriodEnd, CanceledAt: canceledAt}
	if err := service.database.UpsertStripeSubscription(subscription); err != nil {
		return err
	}
	if err := service.database.ApplyEffectiveSubscriptionEntitlement(subscription); err != nil {
		return err
	}
	if service.analyticsEnabled(userID) {
		properties := telemetry.SubscriptionProperties{PlanID: string(tier), Status: subscription.Status, BillingInterval: telemetryInterval(interval)}
		wasActive := previous != nil && db.SubscriptionAllowsPaidAccess(previous.Status)
		isActive := db.SubscriptionAllowsPaidAccess(subscription.Status)
		if !wasActive && isActive {
			service.telemetry.SubscriptionStarted(userID, properties)
		}
		if wasActive && !isActive {
			properties.Status = "canceled"
			service.telemetry.SubscriptionCanceled(userID, properties)
		}
	}
	isActive := db.SubscriptionAllowsPaidAccess(subscription.Status)
	if isActive {
		if _, err := service.database.StartSubscriptionCreditPeriod(userID, tier, time.Now(), event.ID+":activation"); err != nil {
			return err
		}
	}
	effectiveLicense, err := service.database.GetLicenseByUserID(userID)
	if err != nil || effectiveLicense == nil {
		return err
	}
	_, err = service.database.GetOrCreateCreditWallet(userID, effectiveLicense.Tier, time.Now())
	return err
}

func (service *StripeService) handleInvoicePaid(invoice *invoiceEvent) error {
	subscriptionID := strings.TrimSpace(invoice.Subscription)
	if subscriptionID == "" {
		subscriptionID = strings.TrimSpace(invoice.Parent.SubscriptionDetails.Subscription)
	}
	if subscriptionID == "" {
		return nil
	}
	subscription, err := service.database.GetStripeSubscriptionByStripeID(subscriptionID)
	if err != nil || subscription == nil {
		return err
	}
	if _, err := service.database.GetOrCreateCreditWallet(subscription.UserID, subscription.Tier, time.Now()); err != nil {
		return err
	}
	if invoice.BillingReason == "subscription_cycle" && service.analyticsEnabled(subscription.UserID) {
		service.telemetry.SubscriptionRenewed(subscription.UserID, telemetry.SubscriptionProperties{
			PlanID: string(subscription.Tier), Currency: strings.ToLower(invoice.Currency), AmountMinor: invoice.AmountPaid,
			Status: subscription.Status, BillingInterval: telemetryInterval(subscription.BillingInterval),
		})
	}
	return nil
}

func telemetryInterval(interval string) string {
	if interval == "year" {
		return "yearly"
	}
	if interval == "month" {
		return "monthly"
	}
	return interval
}

func configuredSubscriptionPrice(priceID string) (db.Tier, BillingInterval, bool) {
	prices := []struct {
		env      string
		tier     db.Tier
		interval BillingInterval
	}{
		{"STRIPE_PRICE_PRO_MONTHLY", db.TierPro, BillingIntervalMonth},
		{"STRIPE_PRICE_PRO_YEARLY", db.TierPro, BillingIntervalYear},
		{"STRIPE_PRICE_MAX_MONTHLY", db.TierMax, BillingIntervalMonth},
		{"STRIPE_PRICE_MAX_YEARLY", db.TierMax, BillingIntervalYear},
	}
	for _, price := range prices {
		if configured := strings.TrimSpace(os.Getenv(price.env)); configured != "" && configured == strings.TrimSpace(priceID) {
			return price.tier, price.interval, true
		}
	}
	return "", "", false
}

func (service *StripeService) handleCheckoutCompleted(session *checkoutCompletedEvent) {
	if session.Mode != "payment" {
		return
	}

	userID := strings.TrimSpace(session.Metadata["user_id"])
	licenseID := strings.TrimSpace(session.Metadata["license_id"])
	tier, ok := legacyTierFromMetadata(session.Metadata["tier"])
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

	if service.isReplayedCompletedCheckoutAfterReversal(session) {
		log.Printf("Ignoring replayed checkout completion for reversed session %s", session.ID)
		return
	}
	existingPurchase, _ := service.database.GetStripePurchaseByCheckoutSessionID(strings.TrimSpace(session.ID))
	alreadyCaptured := !shouldCaptureSubscriptionStart(existingPurchase)

	if err := service.database.SetLicenseStateByID(licenseID, tier, db.LicenseStatusActive, nil); err != nil {
		log.Printf("Failed to activate %s license %s: %v", tier, licenseID, err)
		return
	}
	if err := service.database.SetLegacyTierByID(licenseID, &tier); err != nil {
		log.Printf("Failed to persist lifetime tier for license %s: %v", licenseID, err)
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
	if !alreadyCaptured && service.analyticsEnabled(userID) {
		service.telemetry.SubscriptionStarted(userID, telemetry.SubscriptionProperties{
			PlanID: string(tier), Currency: strings.ToLower(strings.TrimSpace(session.Currency)), AmountMinor: session.AmountTotal,
			Status: "active", BillingInterval: "lifetime",
		})
	}

	log.Printf("Provisioned %s license for user %s", tier, userID)
}

func shouldCaptureSubscriptionStart(existing *db.StripePurchase) bool {
	return existing == nil || existing.Status != stripePurchaseStatusCompleted
}

func (service *StripeService) isReplayedCompletedCheckoutAfterReversal(session *checkoutCompletedEvent) bool {
	purchase, err := service.database.GetStripePurchaseByCheckoutSessionID(strings.TrimSpace(session.ID))
	if err != nil {
		log.Printf("Failed to check existing purchase for session %s: %v", session.ID, err)
		return false
	}
	if purchase == nil {
		purchase, err = service.database.GetStripePurchaseByPaymentIntent(strings.TrimSpace(session.PaymentIntent))
		if err != nil {
			log.Printf("Failed to check existing purchase for payment intent %s: %v", session.PaymentIntent, err)
			return false
		}
	}
	return purchase != nil && (purchase.Status == stripePurchaseStatusRefunded || purchase.Status == stripePurchaseStatusDisputed)
}

func (service *StripeService) handleChargeRefunded(charge *refundedChargeEvent) {
	creditPurchase, creditErr := service.database.GetCreditPurchaseByPaymentIntent(strings.TrimSpace(charge.PaymentIntent))
	if creditErr != nil {
		log.Printf("Failed to find credit purchase for refund: %v", creditErr)
		return
	}
	if creditPurchase != nil {
		if err := service.database.RefundCreditPurchase(creditPurchase); err != nil {
			log.Printf("Failed to refund credit purchase %s: %v", creditPurchase.ID, err)
		}
		return
	}
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
	if purchase.Status == stripePurchaseStatusRefunded {
		return
	}

	if err := service.database.UpdateStripePurchaseStatus(purchase.ID, stripePurchaseStatusRefunded, "charge.refunded"); err != nil {
		log.Printf("Failed to mark purchase %s refunded: %v", purchase.ID, err)
		return
	}
	if err := service.database.SetLicenseStateByID(purchase.LicenseID, db.TierBasic, db.LicenseStatusActive, nil); err != nil {
		log.Printf("Failed to downgrade license %s after refund: %v", purchase.LicenseID, err)
	}
	_ = service.database.SetLegacyTierByID(purchase.LicenseID, nil)
	if service.analyticsEnabled(purchase.UserID) {
		service.telemetry.SubscriptionCanceled(purchase.UserID, telemetry.SubscriptionProperties{PlanID: string(purchase.TierPurchased), Currency: purchase.Currency, AmountMinor: purchase.Amount, Status: "canceled", BillingInterval: "lifetime"})
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
	if purchase.Status == stripePurchaseStatusDisputed {
		return
	}

	if err := service.database.UpdateStripePurchaseStatus(purchase.ID, stripePurchaseStatusDisputed, "charge.dispute.created"); err != nil {
		log.Printf("Failed to mark purchase %s disputed: %v", purchase.ID, err)
		return
	}
	if err := service.database.SetLicenseStateByID(purchase.LicenseID, db.TierBasic, db.LicenseStatusActive, nil); err != nil {
		log.Printf("Failed to downgrade license %s after dispute: %v", purchase.LicenseID, err)
	}
	_ = service.database.SetLegacyTierByID(purchase.LicenseID, nil)
	if service.analyticsEnabled(purchase.UserID) {
		service.telemetry.SubscriptionCanceled(purchase.UserID, telemetry.SubscriptionProperties{PlanID: string(purchase.TierPurchased), Currency: purchase.Currency, AmountMinor: purchase.Amount, Status: "canceled", BillingInterval: "lifetime"})
	}
}

func (service *StripeService) analyticsEnabled(userID string) bool {
	settings, err := service.database.GetUserSettingsByID(userID)
	return err == nil && settings != nil && settings.AnalyticsEnabled
}

func tierFromMetadata(value string) (db.Tier, bool) {
	switch db.Tier(strings.ToLower(strings.TrimSpace(value))) {
	case db.TierPersonal:
		return db.TierPro, true
	case db.TierPro:
		return db.TierPro, true
	case db.TierMax:
		return db.TierMax, true
	default:
		return "", false
	}
}

func legacyTierFromMetadata(value string) (db.Tier, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "personal":
		return db.TierPro, true
	case "pro", "max":
		return db.TierMax, true
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
