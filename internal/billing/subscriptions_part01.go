package billing

import (
	"errors"
	"time"

	db "github.com/kannachi323/misty/server/internal/platform/postgres"
	"github.com/stripe/stripe-go/v82"
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
