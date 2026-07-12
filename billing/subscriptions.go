package billing

import (
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

const (
	CreditPackSmall = "credits_1500"
	CreditPackLarge = "credits_3500"
)

var (
	ErrInvalidTier        = errors.New("invalid tier")
	ErrInvalidInterval    = errors.New("invalid billing interval")
	ErrInvalidCreditPack  = errors.New("invalid credit pack")
	ErrUserNotFound       = errors.New("user not found")
	ErrLicenseNotFound    = errors.New("license not found")
	ErrTrialUnavailable   = errors.New("trial unavailable")
	ErrSubscriptionExists = errors.New("active subscription already exists")
	ErrPortalUnavailable  = errors.New("customer portal unavailable")
)

type priceKey struct {
	tier     db.Tier
	interval BillingInterval
}

type checkoutConfig struct {
	secretKey       string
	successURL      string
	cancelURL       string
	portalReturnURL string
	prices          map[priceKey]string
	creditPrices    map[string]string
}

type CheckoutSessionCreator func(cfg checkoutConfig, user *db.User, tier db.Tier, interval BillingInterval, customerID string) (string, error)
type CreditCheckoutSessionCreator func(cfg checkoutConfig, user *db.User, packID, customerID string) (string, error)
type PortalSessionCreator func(secretKey, customerID, returnURL string) (string, error)

type Service struct {
	database             *db.Database
	createCheckout       CheckoutSessionCreator
	createCreditCheckout CreditCheckoutSessionCreator
	createPortal         PortalSessionCreator
	trialDuration        time.Duration
}

type ServiceOption func(*Service)

func NewService(database *db.Database, opts ...ServiceOption) *Service {
	service := &Service{database: database, createCheckout: createStripeCheckoutSession,
		createCreditCheckout: createStripeCreditCheckoutSession, createPortal: createStripePortalSession,
		trialDuration: ProTrialDuration}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func WithCheckoutSessionCreator(fn CheckoutSessionCreator) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.createCheckout = fn
		}
	}
}

func WithCreditCheckoutSessionCreator(fn CreditCheckoutSessionCreator) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.createCreditCheckout = fn
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

func validPaidTier(tier db.Tier) bool { return tier == db.TierPro || tier == db.TierMax }

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
	cfg, err := loadStripeCheckoutConfig()
	if err != nil {
		return "", err
	}
	customerID := ""
	if existing != nil {
		customerID = existing.StripeCustomerID
	}
	return service.createCheckout(cfg, user, tier, interval, customerID)
}

func (service *Service) CreateCreditCheckoutSession(userID, packID string) (string, error) {
	if packCredits(packID) == 0 {
		return "", ErrInvalidCreditPack
	}
	user, err := service.database.GetUserByID(userID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", ErrUserNotFound
	}
	cfg, err := loadStripeCheckoutConfig()
	if err != nil {
		return "", err
	}
	customerID, err := service.database.GetStripeCustomerIDForUser(userID)
	if err != nil {
		return "", err
	}
	return service.createCreditCheckout(cfg, user, packID, customerID)
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

func createStripeCheckoutSession(cfg checkoutConfig, user *db.User, tier db.Tier, interval BillingInterval, customerID string) (string, error) {
	stripe.Key = cfg.secretKey
	metadata := map[string]string{"user_id": user.ID, "license_id": user.LicenseID, "tier": string(tier), "interval": string(interval), "kind": "subscription"}
	params := &stripe.CheckoutSessionParams{Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL: stripe.String(cfg.successURL), CancelURL: stripe.String(cfg.cancelURL), ClientReferenceID: stripe.String(user.ID),
		Metadata: metadata, SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{Metadata: metadata},
		LineItems: []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(cfg.prices[priceKey{tier: tier, interval: interval}]), Quantity: stripe.Int64(1)}}}
	if customerID != "" {
		params.Customer = stripe.String(customerID)
	} else {
		params.CustomerEmail = stripe.String(user.Email)
	}
	session, err := checkoutsession.New(params)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

func createStripeCreditCheckoutSession(cfg checkoutConfig, user *db.User, packID, customerID string) (string, error) {
	stripe.Key = cfg.secretKey
	metadata := map[string]string{"user_id": user.ID, "license_id": user.LicenseID, "kind": "credit_pack", "pack_id": packID}
	params := &stripe.CheckoutSessionParams{Mode: stripe.String(string(stripe.CheckoutSessionModePayment)), SuccessURL: stripe.String(cfg.successURL),
		CancelURL: stripe.String(cfg.cancelURL), ClientReferenceID: stripe.String(user.ID), Metadata: metadata,
		LineItems: []*stripe.CheckoutSessionLineItemParams{{Price: stripe.String(cfg.creditPrices[packID]), Quantity: stripe.Int64(1)}}}
	if customerID != "" {
		params.Customer = stripe.String(customerID)
	} else {
		params.CustomerEmail = stripe.String(user.Email)
	}
	session, err := checkoutsession.New(params)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

func createStripePortalSession(secretKey, customerID, returnURL string) (string, error) {
	stripe.Key = secretKey
	session, err := portalsession.New(&stripe.BillingPortalSessionParams{Customer: stripe.String(customerID), ReturnURL: stripe.String(returnURL)})
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

func loadStripeCheckoutConfig() (checkoutConfig, error) {
	cfg := checkoutConfig{secretKey: strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")),
		successURL: strings.TrimSpace(os.Getenv("STRIPE_CHECKOUT_SUCCESS_URL")), cancelURL: strings.TrimSpace(os.Getenv("STRIPE_CHECKOUT_CANCEL_URL")),
		portalReturnURL: strings.TrimSpace(os.Getenv("STRIPE_PORTAL_RETURN_URL")),
		prices: map[priceKey]string{
			{tier: db.TierPro, interval: BillingIntervalMonth}: strings.TrimSpace(os.Getenv("STRIPE_PRICE_PRO_MONTHLY")),
			{tier: db.TierPro, interval: BillingIntervalYear}:  strings.TrimSpace(os.Getenv("STRIPE_PRICE_PRO_YEARLY")),
			{tier: db.TierMax, interval: BillingIntervalMonth}: strings.TrimSpace(os.Getenv("STRIPE_PRICE_MAX_MONTHLY")),
			{tier: db.TierMax, interval: BillingIntervalYear}:  strings.TrimSpace(os.Getenv("STRIPE_PRICE_MAX_YEARLY")),
		}, creditPrices: map[string]string{
			CreditPackSmall: strings.TrimSpace(os.Getenv("STRIPE_PRICE_CREDITS_1500")), CreditPackLarge: strings.TrimSpace(os.Getenv("STRIPE_PRICE_CREDITS_3500")),
		}}
	required := []struct{ name, value string }{{"STRIPE_SECRET_KEY", cfg.secretKey}, {"STRIPE_CHECKOUT_SUCCESS_URL", cfg.successURL},
		{"STRIPE_CHECKOUT_CANCEL_URL", cfg.cancelURL}, {"STRIPE_PORTAL_RETURN_URL", cfg.portalReturnURL},
		{"STRIPE_PRICE_PRO_MONTHLY", cfg.prices[priceKey{db.TierPro, BillingIntervalMonth}]}, {"STRIPE_PRICE_PRO_YEARLY", cfg.prices[priceKey{db.TierPro, BillingIntervalYear}]},
		{"STRIPE_PRICE_MAX_MONTHLY", cfg.prices[priceKey{db.TierMax, BillingIntervalMonth}]}, {"STRIPE_PRICE_MAX_YEARLY", cfg.prices[priceKey{db.TierMax, BillingIntervalYear}]},
		{"STRIPE_PRICE_CREDITS_1500", cfg.creditPrices[CreditPackSmall]}, {"STRIPE_PRICE_CREDITS_3500", cfg.creditPrices[CreditPackLarge]}}
	for _, item := range required {
		if item.value == "" {
			return checkoutConfig{}, &configError{name: item.name}
		}
	}
	return cfg, nil
}

func packCredits(packID string) int64 {
	switch packID {
	case CreditPackSmall:
		return 1_500 * db.CreditDenominationScale
	case CreditPackLarge:
		return 3_500 * db.CreditDenominationScale
	default:
		return 0
	}
}

func packAmountMinor(packID string) int64 {
	switch packID {
	case CreditPackSmall:
		return 499
	case CreditPackLarge:
		return 999
	default:
		return 0
	}
}

type configError struct{ name string }

func (e *configError) Error() string { return e.name + " is required" }
