package billing

import (
	"context"
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
	subscriptionapi "github.com/stripe/stripe-go/v82/subscription"
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
type SubscriptionFetcher func(subscriptionID string) (*stripe.Subscription, error)

type StripeService struct {
	database          *db.Database
	fetchChargeID     ChargeIDFetcher
	fetchSubscription SubscriptionFetcher
	telemetry         telemetry.Client
}

type StripeOption func(*StripeService)

func NewStripeService(database *db.Database, opts ...StripeOption) *StripeService {
	service := &StripeService{
		database:          database,
		fetchChargeID:     fetchChargeIDFromStripe,
		fetchSubscription: fetchSubscriptionFromStripe,
		telemetry:         telemetry.NoopClient{},
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

func WithSubscriptionFetcher(fn SubscriptionFetcher) StripeOption {
	return func(service *StripeService) {
		if fn != nil {
			service.fetchSubscription = fn
		}
	}
}

type SubscriptionReconcileReport struct {
	Checked             int
	Updated             int
	Failed              int
	EntitlementsExpired int
}

func (service *StripeService) ReconcileSubscriptions(
	ctx context.Context,
	now time.Time,
	limit int,
) (SubscriptionReconcileReport, error) {
	var report SubscriptionReconcileReport
	subscriptions, err := service.database.ListStripeSubscriptionsDueForReconciliation(
		ctx,
		now,
		limit,
	)
	if err != nil {
		return report, err
	}
	for _, local := range subscriptions {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		report.Checked++
		canonical, fetchErr := service.fetchSubscription(local.StripeSubscriptionID)
		if fetchErr != nil || canonical == nil {
			report.Failed++
			failure := "Stripe returned an empty subscription"
			if fetchErr != nil {
				failure = fetchErr.Error()
			}
			retryAt := now.Add(subscriptionReconcileBackoff(local.ReconcileFailures))
			if markErr := service.database.MarkStripeSubscriptionReconcileFailed(
				ctx,
				local.StripeSubscriptionID,
				failure,
				retryAt,
			); markErr != nil {
				return report, markErr
			}
			continue
		}
		event, conversionErr := subscriptionEventFromStripe(canonical)
		if conversionErr != nil {
			report.Failed++
			retryAt := now.Add(subscriptionReconcileBackoff(local.ReconcileFailures))
			if markErr := service.database.MarkStripeSubscriptionReconcileFailed(
				ctx,
				local.StripeSubscriptionID,
				conversionErr.Error(),
				retryAt,
			); markErr != nil {
				return report, markErr
			}
			continue
		}
		if applyErr := service.handleSubscriptionChanged(
			event,
			"",
			time.Time{},
			false,
		); applyErr != nil {
			report.Failed++
			retryAt := now.Add(subscriptionReconcileBackoff(local.ReconcileFailures))
			if markErr := service.database.MarkStripeSubscriptionReconcileFailed(
				ctx,
				local.StripeSubscriptionID,
				applyErr.Error(),
				retryAt,
			); markErr != nil {
				return report, markErr
			}
			continue
		}
		reconcileAt := nextSubscriptionReconcileAt(now, periodEndFromSubscriptionEvent(event))
		if markErr := service.database.MarkStripeSubscriptionReconciled(
			ctx,
			local.StripeSubscriptionID,
			now,
			reconcileAt,
		); markErr != nil {
			return report, markErr
		}
		report.Updated++
	}
	expired, err := service.database.ExpireStaleSubscriptionEntitlements(
		ctx,
		now.Add(-72*time.Hour),
		limit,
	)
	if err != nil {
		return report, err
	}
	report.EntitlementsExpired = expired
	return report, nil
}

func subscriptionReconcileBackoff(priorFailures int) time.Duration {
	if priorFailures < 0 {
		priorFailures = 0
	}
	if priorFailures > 5 {
		priorFailures = 5
	}
	return 15 * time.Minute * time.Duration(1<<priorFailures)
}

func (service *StripeService) HandleWebhookEvent(eventType string, payload json.RawMessage) {
	if err := service.HandleWebhookEventWithID("", eventType, payload); err != nil {
		log.Printf("Stripe webhook %s failed: %v", eventType, err)
	}
}

func (service *StripeService) HandleWebhookEventWithID(eventID, eventType string, payload json.RawMessage) error {
	return service.HandleWebhookEventAt(eventID, eventType, time.Time{}, payload)
}

func (service *StripeService) HandleWebhookEventAt(
	eventID, eventType string,
	eventCreatedAt time.Time,
	payload json.RawMessage,
) error {
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
	case "checkout.session.expired":
		var session checkoutCompletedEvent
		if err := json.Unmarshal(payload, &session); err != nil {
			return err
		}
		if err := service.database.ExpireSubscriptionCheckoutBySessionID(
			context.Background(),
			session.ID,
		); err != nil {
			return err
		}
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var subscription subscriptionEvent
		if err := json.Unmarshal(payload, &subscription); err != nil {
			return err
		}
		if err := service.handleSubscriptionChanged(
			&subscription,
			eventID,
			eventCreatedAt,
			true,
		); err != nil {
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
		if err := service.database.CompleteSubscriptionCheckoutBySessionID(
			context.Background(),
			session.ID,
		); err != nil {
			return err
		}
		if strings.TrimSpace(session.Subscription) == "" {
			// customer.subscription.* remains the normal entitlement path.
			return nil
		}
		canonical, err := service.fetchSubscription(session.Subscription)
		if err != nil {
			return err
		}
		event, err := subscriptionEventFromStripe(canonical)
		if err != nil {
			return err
		}
		return service.handleSubscriptionChanged(
			event,
			"",
			time.Time{},
			false,
		)
	}
	// One-time lifetime products and credit packs are retired. Acknowledging
	// stale events prevents Stripe retries without granting an entitlement.
	log.Printf("Ignoring retired one-time checkout event for session %s", session.ID)
	return nil
}

func (service *StripeService) handleSubscriptionChanged(
	event *subscriptionEvent,
	sourceEventID string,
	sourceEventCreatedAt time.Time,
	rejectOlder bool,
) error {
	userID := strings.TrimSpace(event.Metadata["user_id"])
	licenseID := strings.TrimSpace(event.Metadata["license_id"])
	tier, ok := tierFromMetadata(event.Metadata["tier"])
	metadataInterval := BillingInterval(strings.ToLower(strings.TrimSpace(event.Metadata["interval"])))
	if userID == "" || licenseID == "" || !ok || !validPaidTier(tier) ||
		!validInterval(metadataInterval) || strings.TrimSpace(event.Metadata["kind"]) != "subscription" {
		return errors.New("subscription metadata is invalid")
	}
	user, err := service.database.GetUserByID(userID)
	if err != nil {
		return err
	}
	if user == nil || user.LicenseID != licenseID {
		return errors.New("subscription license does not match user")
	}
	if len(event.Items.Data) != 1 {
		return errors.New("subscription must contain exactly one configured price")
	}
	priceID := strings.TrimSpace(event.Items.Data[0].Price.ID)
	catalogTier, catalogInterval, catalogOK := configuredSubscriptionPrice(priceID)
	if !catalogOK {
		return errors.New("subscription price is not in the configured catalog")
	}
	if catalogTier != tier {
		return errors.New("subscription tier metadata does not match the Stripe price")
	}
	if catalogInterval != metadataInterval {
		return errors.New("subscription interval metadata does not match the Stripe price")
	}
	if itemInterval := strings.ToLower(strings.TrimSpace(event.Items.Data[0].Price.Recurring.Interval)); itemInterval != "" && itemInterval != string(catalogInterval) {
		return errors.New("subscription recurring interval does not match the Stripe price")
	}
	tier = catalogTier
	interval := string(catalogInterval)
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
	status := strings.ToLower(strings.TrimSpace(event.Status))
	if db.SubscriptionAllowsPaidAccess(status) && periodEnd == nil {
		return errors.New("paid Stripe subscription has no billing period end")
	}
	var sourceCreated *time.Time
	if !sourceEventCreatedAt.IsZero() {
		value := sourceEventCreatedAt.UTC()
		sourceCreated = &value
	}
	now := time.Now().UTC()
	previous, err := service.database.GetStripeSubscriptionByStripeID(event.ID)
	if err != nil {
		return err
	}
	subscription := &db.StripeSubscription{UserID: userID, LicenseID: licenseID, StripeSubscriptionID: event.ID,
		StripeCustomerID: event.Customer, StripePriceID: priceID, Tier: tier, BillingInterval: interval,
		Status: status, CurrentPeriodEnd: periodEnd,
		CancelAtPeriodEnd: event.CancelAtPeriodEnd, CanceledAt: canceledAt,
		SourceEventCreatedAt: sourceCreated, SourceEventID: sourceEventID,
		ReconcileAfter: nextSubscriptionReconcileAt(now, periodEnd)}
	applied := true
	if rejectOlder {
		applied, err = service.database.UpsertStripeSubscriptionFromWebhook(subscription)
	} else {
		err = service.database.UpsertStripeSubscription(subscription)
	}
	if err != nil {
		return err
	}
	if !applied {
		return nil
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

func nextSubscriptionReconcileAt(now time.Time, periodEnd *time.Time) time.Time {
	next := now.UTC().Add(6 * time.Hour)
	if periodEnd == nil {
		return next
	}
	nearPeriodEnd := periodEnd.UTC().Add(15 * time.Minute)
	if nearPeriodEnd.Before(now) {
		nearPeriodEnd = now.UTC().Add(15 * time.Minute)
	}
	if nearPeriodEnd.Before(next) {
		return nearPeriodEnd
	}
	return next
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
	var matched *priceKey
	for _, definition := range subscriptionPriceDefinitions {
		if configured := strings.TrimSpace(os.Getenv(definition.env)); configured != "" && configured == strings.TrimSpace(priceID) {
			if matched != nil {
				return "", "", false
			}
			key := definition.key
			matched = &key
		}
	}
	if matched == nil {
		return "", "", false
	}
	return matched.tier, matched.interval, true
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
		return db.TierPro, true
	default:
		return "", false
	}
}

func subscriptionEventFromStripe(
	subscription *stripe.Subscription,
) (*subscriptionEvent, error) {
	if subscription == nil || strings.TrimSpace(subscription.ID) == "" {
		return nil, errors.New("Stripe returned an invalid subscription")
	}
	if subscription.Customer == nil ||
		strings.TrimSpace(subscription.Customer.ID) == "" {
		return nil, errors.New("Stripe subscription has no customer")
	}
	event := &subscriptionEvent{
		ID:                subscription.ID,
		Customer:          subscription.Customer.ID,
		Status:            string(subscription.Status),
		Metadata:          subscription.Metadata,
		CancelAtPeriodEnd: subscription.CancelAtPeriodEnd,
		CanceledAt:        subscription.CanceledAt,
	}
	if subscription.Items == nil || len(subscription.Items.Data) != 1 {
		return nil, errors.New("Stripe subscription must contain exactly one item")
	}
	item := subscription.Items.Data[0]
	if item == nil || item.Price == nil {
		return nil, errors.New("Stripe subscription item has no price")
	}
	itemEvent := struct {
		CurrentPeriodEnd int64 `json:"current_period_end"`
		Price            struct {
			ID        string `json:"id"`
			Recurring struct {
				Interval string `json:"interval"`
			} `json:"recurring"`
		} `json:"price"`
	}{}
	itemEvent.CurrentPeriodEnd = item.CurrentPeriodEnd
	itemEvent.Price.ID = item.Price.ID
	if item.Price.Recurring != nil {
		itemEvent.Price.Recurring.Interval = string(item.Price.Recurring.Interval)
	}
	event.Items.Data = append(event.Items.Data, itemEvent)
	return event, nil
}

func periodEndFromSubscriptionEvent(event *subscriptionEvent) *time.Time {
	if event == nil {
		return nil
	}
	raw := event.CurrentPeriodEnd
	if raw <= 0 && len(event.Items.Data) > 0 {
		raw = event.Items.Data[0].CurrentPeriodEnd
	}
	if raw <= 0 {
		return nil
	}
	value := time.Unix(raw, 0).UTC()
	return &value
}

func fetchSubscriptionFromStripe(subscriptionID string) (*stripe.Subscription, error) {
	secretKey := strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY"))
	if secretKey == "" {
		return nil, errors.New("STRIPE_SECRET_KEY is required for subscription reconciliation")
	}
	if strings.TrimSpace(subscriptionID) == "" {
		return nil, errors.New("Stripe subscription id is required")
	}
	stripe.Key = secretKey
	return subscriptionapi.Get(subscriptionID, nil)
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
