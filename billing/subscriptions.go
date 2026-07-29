package billing

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/kannachi323/misty/server/db"
	"github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
)

const ProTrialDuration = 14 * 24 * time.Hour

type BillingInterval string

const (
	BillingIntervalMonth BillingInterval = "month"
	BillingIntervalYear  BillingInterval = "year"
)

var (
	ErrInvalidTier        = errors.New("invalid tier")
	ErrInvalidInterval    = errors.New("invalid billing interval")
	ErrInvalidCreditPack  = errors.New("hosted AI add-ons are retired")
	ErrUserNotFound       = errors.New("user not found")
	ErrLicenseNotFound    = errors.New("license not found")
	ErrTrialUnavailable   = errors.New("trial unavailable")
	ErrSubscriptionExists = errors.New("active subscription already exists")
	ErrCheckoutInProgress = errors.New("subscription checkout already in progress")
	ErrPortalUnavailable  = errors.New("customer portal unavailable")
)

type priceKey struct {
	tier     db.Tier
	interval BillingInterval
}

var subscriptionPriceDefinitions = []struct {
	env string
	key priceKey
}{
	{"STRIPE_PRICE_PRO_MONTHLY", priceKey{db.TierPro, BillingIntervalMonth}},
	{"STRIPE_PRICE_PRO_YEARLY", priceKey{db.TierPro, BillingIntervalYear}},
	{"STRIPE_PRICE_MAX_MONTHLY", priceKey{db.TierMax, BillingIntervalMonth}},
	{"STRIPE_PRICE_MAX_YEARLY", priceKey{db.TierMax, BillingIntervalYear}},
}

type CheckoutConfig struct {
	secretKey       string
	successURL      string
	cancelURL       string
	portalReturnURL string
	prices          map[priceKey]string
}

type CheckoutSessionResult struct {
	ID        string
	URL       string
	ExpiresAt time.Time
}

type CheckoutSessionCreator func(
	cfg CheckoutConfig,
	user *db.User,
	tier db.Tier,
	interval BillingInterval,
	customerID string,
	trialEligible bool,
	idempotencyKey string,
	expiresAt time.Time,
) (CheckoutSessionResult, error)
type CheckoutSessionFetcher func(
	cfg CheckoutConfig,
	sessionID string,
) (*stripe.CheckoutSession, error)
type PortalSessionCreator func(secretKey, customerID, returnURL string) (string, error)

type Service struct {
	database       *db.Database
	createCheckout CheckoutSessionCreator
	fetchCheckout  CheckoutSessionFetcher
	createPortal   PortalSessionCreator
	trialDuration  time.Duration
}

type ServiceOption func(*Service)

func NewService(database *db.Database, opts ...ServiceOption) *Service {
	service := &Service{database: database, createCheckout: createStripeCheckoutSession, createPortal: createStripePortalSession,
		fetchCheckout: fetchStripeCheckoutSession, trialDuration: ProTrialDuration}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func WithCheckoutSessionFetcher(fn CheckoutSessionFetcher) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.fetchCheckout = fn
		}
	}
}

func WithCheckoutSessionCreator(fn CheckoutSessionCreator) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.createCheckout = fn
		}
	}
}

func WithPortalSessionCreator(fn PortalSessionCreator) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.createPortal = fn
		}
	}
}

func WithTrialDuration(duration time.Duration) ServiceOption {
	return func(service *Service) {
		if duration > 0 {
			service.trialDuration = duration
		}
	}
}

func validPaidTier(tier db.Tier) bool {
	return tier == db.TierPro || tier == db.TierMax
}

func validInterval(interval BillingInterval) bool {
	return interval == BillingIntervalMonth || interval == BillingIntervalYear
}

func (service *Service) CreateCheckoutSession(userID string, tier db.Tier, interval BillingInterval) (string, error) {
	if !validPaidTier(tier) {
		return "", ErrInvalidTier
	}
	if !validInterval(interval) {
		return "", ErrInvalidInterval
	}
	user, err := service.database.GetUserByID(userID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", ErrUserNotFound
	}
	existing, err := service.database.GetStripeSubscriptionByUserID(userID)
	if err != nil {
		return "", err
	}
	if existing != nil && db.SubscriptionAllowsPaidAccess(existing.Status) {
		return "", ErrSubscriptionExists
	}
	if existing == nil {
		completedCheckout, err := service.database.HasCompletedSubscriptionCheckoutWithoutSubscription(
			context.Background(),
			userID,
		)
		if err != nil {
			return "", err
		}
		if completedCheckout {
			return "", ErrSubscriptionExists
		}
	}
	cfg, err := loadStripeCheckoutConfig()
	if err != nil {
		return "", err
	}
	customerID, err := service.database.GetStripeCustomerIDForUser(userID)
	if err != nil {
		return "", err
	}
	license, err := service.database.GetLicenseByUserID(userID)
	if err != nil {
		return "", err
	}
	hasPurchase, err := service.database.HasCompletedStripePurchase(userID)
	if err != nil {
		return "", err
	}
	// The one-time trial remains a Pro benefit. Max Checkout never receives an
	// automatic trial, even when the account would otherwise be eligible.
	trialEligible := tier == db.TierPro && license != nil && license.TrialStartedAt == nil && !hasPurchase
	attempt, _, err := service.database.BeginSubscriptionCheckout(
		context.Background(),
		user.ID,
		user.LicenseID,
		tier,
		string(interval),
		time.Now(),
		35*time.Minute,
	)
	if err != nil {
		return "", err
	}
	if attempt.Tier != tier || attempt.BillingInterval != string(interval) {
		return "", ErrCheckoutInProgress
	}
	if attempt.Status == "open" && attempt.CheckoutURL != "" &&
		attempt.ExpiresAt.After(time.Now()) {
		return attempt.CheckoutURL, nil
	}
	if attempt.Status == "open" {
		// Resolve an overdue attempt from Stripe before releasing it. This is
		// read-only and covers a delayed or missed completion/expiration event.
		canonical, fetchErr := service.fetchCheckout(
			cfg,
			attempt.StripeCheckoutSessionID,
		)
		if fetchErr != nil || canonical == nil {
			return "", ErrCheckoutInProgress
		}
		switch canonical.Status {
		case stripe.CheckoutSessionStatusComplete:
			if err := service.database.CompleteSubscriptionCheckoutBySessionID(
				context.Background(),
				attempt.StripeCheckoutSessionID,
			); err != nil {
				return "", err
			}
			return "", ErrSubscriptionExists
		case stripe.CheckoutSessionStatusExpired:
			if err := service.database.ExpireSubscriptionCheckoutBySessionID(
				context.Background(),
				attempt.StripeCheckoutSessionID,
			); err != nil {
				return "", err
			}
			return service.CreateCheckoutSession(userID, tier, interval)
		default:
			return "", ErrCheckoutInProgress
		}
	}
	result, err := service.createCheckout(
		cfg,
		user,
		tier,
		interval,
		customerID,
		trialEligible,
		attempt.ID,
		attempt.ExpiresAt,
	)
	if err != nil {
		// Keep the attempt recoverable. A retry uses the same Stripe
		// idempotency key, so an ambiguous network failure cannot create a
		// second Checkout Session.
		return "", err
	}
	if result.ID == "" || result.URL == "" {
		return "", errors.New("Stripe returned an incomplete Checkout Session")
	}
	if result.ExpiresAt.IsZero() {
		result.ExpiresAt = attempt.ExpiresAt
	}
	if err := service.database.OpenSubscriptionCheckout(
		context.Background(),
		attempt.ID,
		result.ID,
		result.URL,
		result.ExpiresAt,
	); err != nil {
		return "", err
	}
	return result.URL, nil
}

func (service *Service) CreateCreditCheckoutSession(userID, packID string) (string, error) {
	return "", ErrInvalidCreditPack
}

func (service *Service) CreatePortalSession(userID string) (string, error) {
	cfg, err := loadStripeCheckoutConfig()
	if err != nil {
		return "", err
	}
	customerID, err := service.database.GetStripeCustomerIDForUser(userID)
	if err != nil {
		return "", err
	}
	if customerID == "" {
		return "", ErrPortalUnavailable
	}
	return service.createPortal(cfg.secretKey, customerID, cfg.portalReturnURL)
}

func (service *Service) StartProTrial(userID string) (*db.License, error) {
	license, err := service.database.GetLicenseByUserID(userID)
	if err != nil {
		return nil, err
	}
	if license == nil {
		return nil, ErrLicenseNotFound
	}
	if license.TrialStartedAt != nil || license.Tier != db.TierBasic || license.Status != db.LicenseStatusActive {
		return nil, ErrTrialUnavailable
	}
	hasPurchase, err := service.database.HasCompletedStripePurchase(userID)
	if err != nil {
		return nil, err
	}
	if hasPurchase {
		return nil, ErrTrialUnavailable
	}
	started, err := service.database.StartTrialByUserID(userID, service.trialDuration)
	if err != nil {
		return nil, err
	}
	if !started {
		return nil, ErrTrialUnavailable
	}
	return service.database.GetLicenseByUserID(userID)
}

// StartPersonalTrial remains as a compatibility shim for existing callers.
func (service *Service) StartPersonalTrial(userID string) (*db.License, error) {
	return service.StartProTrial(userID)
}

func createStripeCheckoutSession(
	cfg CheckoutConfig,
	user *db.User,
	tier db.Tier,
	interval BillingInterval,
	customerID string,
	trialEligible bool,
	idempotencyKey string,
	expiresAt time.Time,
) (CheckoutSessionResult, error) {
	stripe.Key = cfg.secretKey
	params := stripeCheckoutSessionParams(cfg, user, tier, interval, customerID, trialEligible)
	params.ExpiresAt = stripe.Int64(expiresAt.UTC().Unix())
	params.SetIdempotencyKey("misty-subscription-checkout-" + idempotencyKey)
	session, err := checkoutsession.New(params)
	if err != nil {
		return CheckoutSessionResult{}, err
	}
	return CheckoutSessionResult{
		ID: session.ID, URL: session.URL, ExpiresAt: time.Unix(session.ExpiresAt, 0).UTC(),
	}, nil
}

func fetchStripeCheckoutSession(
	cfg CheckoutConfig,
	sessionID string,
) (*stripe.CheckoutSession, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("Stripe Checkout Session id is required")
	}
	stripe.Key = cfg.secretKey
	return checkoutsession.Get(sessionID, nil)
}

func stripeCheckoutSessionParams(cfg CheckoutConfig, user *db.User, tier db.Tier, interval BillingInterval, customerID string, trialEligible bool) *stripe.CheckoutSessionParams {
	metadata := map[string]string{"user_id": user.ID, "license_id": user.LicenseID, "tier": string(tier), "interval": string(interval), "kind": "subscription"}
	params := &stripe.CheckoutSessionParams{Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripe.String(cfg.successURL), CancelURL: stripe.String(cfg.cancelURL), ClientReferenceID: stripe.String(user.ID),
		Metadata: metadata, SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{Metadata: metadata},
		LineItems: []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(cfg.prices[priceKey{tier: tier, interval: interval}]), Quantity: stripe.Int64(1)}}}
	if tier == db.TierPro && trialEligible {
		params.SubscriptionData.TrialPeriodDays = stripe.Int64(int64(ProTrialDuration / (24 * time.Hour)))
		params.PaymentMethodCollection = stripe.String(string(stripe.CheckoutSessionPaymentMethodCollectionAlways))
	}
	if customerID != "" {
		params.Customer = stripe.String(customerID)
	} else {
		params.CustomerEmail = stripe.String(user.Email)
	}
	return params
}

func createStripePortalSession(secretKey, customerID, returnURL string) (string, error) {
	stripe.Key = secretKey
	session, err := portalsession.New(&stripe.BillingPortalSessionParams{Customer: stripe.String(customerID), ReturnURL: stripe.String(returnURL)})
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

func loadStripeCheckoutConfig() (CheckoutConfig, error) {
	cfg := CheckoutConfig{secretKey: strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")),
		successURL: strings.TrimSpace(os.Getenv("STRIPE_CHECKOUT_SUCCESS_URL")), cancelURL: strings.TrimSpace(os.Getenv("STRIPE_CHECKOUT_CANCEL_URL")),
		portalReturnURL: strings.TrimSpace(os.Getenv("STRIPE_PORTAL_RETURN_URL")),
		prices:          make(map[priceKey]string, len(subscriptionPriceDefinitions))}
	for _, definition := range subscriptionPriceDefinitions {
		cfg.prices[definition.key] = strings.TrimSpace(os.Getenv(definition.env))
	}
	required := []struct{ name, value string }{{"STRIPE_SECRET_KEY", cfg.secretKey}, {"STRIPE_CHECKOUT_SUCCESS_URL", cfg.successURL},
		{"STRIPE_CHECKOUT_CANCEL_URL", cfg.cancelURL}, {"STRIPE_PORTAL_RETURN_URL", cfg.portalReturnURL}}
	for _, definition := range subscriptionPriceDefinitions {
		required = append(required, struct{ name, value string }{definition.env, cfg.prices[definition.key]})
	}
	for _, item := range required {
		if item.value == "" {
			return CheckoutConfig{}, &configError{name: item.name}
		}
	}
	seenPrices := make(map[string]priceKey, len(cfg.prices))
	for key, priceID := range cfg.prices {
		if _, exists := seenPrices[priceID]; exists {
			return CheckoutConfig{}, errors.New("Stripe subscription Price IDs must be unique")
		}
		seenPrices[priceID] = key
	}
	return cfg, nil
}

type configError struct{ name string }

func (e *configError) Error() string { return e.name + " is required" }
