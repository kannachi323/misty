package billing

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/kannachi323/misty/server/db"
	"github.com/stripe/stripe-go/v82"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
)

const PersonalTrialDuration = 14 * 24 * time.Hour

var (
	ErrInvalidTier      = errors.New("invalid tier")
	ErrUserNotFound     = errors.New("user not found")
	ErrLicenseNotFound  = errors.New("license not found")
	ErrTrialUnavailable = errors.New("trial unavailable")
)

type checkoutConfig struct {
	secretKey  string
	successURL string
	cancelURL  string
	prices     map[db.Tier]string
}

type CheckoutSessionCreator func(cfg checkoutConfig, user *db.User, tier db.Tier) (string, error)

type Service struct {
	database              *db.Database
	createCheckoutSession CheckoutSessionCreator
	trialDuration         time.Duration
}

type ServiceOption func(*Service)

func NewService(database *db.Database, opts ...ServiceOption) *Service {
	service := &Service{
		database:              database,
		createCheckoutSession: createStripeCheckoutSession,
		trialDuration:         PersonalTrialDuration,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func WithCheckoutSessionCreator(fn CheckoutSessionCreator) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.createCheckoutSession = fn
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

func (service *Service) CreateCheckoutSession(userID string, tier db.Tier) (string, error) {
	if tier != db.TierPersonal && tier != db.TierPro {
		return "", ErrInvalidTier
	}

	user, err := service.database.GetUserByID(userID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", ErrUserNotFound
	}

	config, err := loadStripeCheckoutConfig()
	if err != nil {
		return "", err
	}

	return service.createCheckoutSession(config, user, tier)
}

func (service *Service) StartPersonalTrial(userID string) (*db.License, error) {
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

	updatedLicense, err := service.database.GetLicenseByUserID(userID)
	if err != nil {
		return nil, err
	}
	if updatedLicense == nil {
		return nil, ErrLicenseNotFound
	}

	return updatedLicense, nil
}

func createStripeCheckoutSession(cfg checkoutConfig, user *db.User, tier db.Tier) (string, error) {
	stripe.Key = cfg.secretKey

	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL:        stripe.String(cfg.successURL),
		CancelURL:         stripe.String(cfg.cancelURL),
		ClientReferenceID: stripe.String(user.ID),
		CustomerEmail:     stripe.String(user.Email),
		Metadata: map[string]string{
			"user_id":    user.ID,
			"license_id": user.LicenseID,
			"tier":       string(tier),
		},
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(cfg.prices[tier]),
				Quantity: stripe.Int64(1),
			},
		},
	}

	session, err := checkoutsession.New(params)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}

func loadStripeCheckoutConfig() (checkoutConfig, error) {
	cfg := checkoutConfig{
		secretKey:  strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")),
		successURL: strings.TrimSpace(os.Getenv("STRIPE_CHECKOUT_SUCCESS_URL")),
		cancelURL:  strings.TrimSpace(os.Getenv("STRIPE_CHECKOUT_CANCEL_URL")),
		prices: map[db.Tier]string{
			db.TierPersonal: strings.TrimSpace(os.Getenv("STRIPE_PRICE_PERSONAL")),
			db.TierPro:      strings.TrimSpace(os.Getenv("STRIPE_PRICE_PRO")),
		},
	}

	switch {
	case cfg.secretKey == "":
		return checkoutConfig{}, errMissingStripeConfig("STRIPE_SECRET_KEY")
	case cfg.successURL == "":
		return checkoutConfig{}, errMissingStripeConfig("STRIPE_CHECKOUT_SUCCESS_URL")
	case cfg.cancelURL == "":
		return checkoutConfig{}, errMissingStripeConfig("STRIPE_CHECKOUT_CANCEL_URL")
	case cfg.prices[db.TierPersonal] == "":
		return checkoutConfig{}, errMissingStripeConfig("STRIPE_PRICE_PERSONAL")
	case cfg.prices[db.TierPro] == "":
		return checkoutConfig{}, errMissingStripeConfig("STRIPE_PRICE_PRO")
	default:
		return cfg, nil
	}
}

func errMissingStripeConfig(name string) error {
	return &configError{name: name}
}

type configError struct {
	name string
}

func (e *configError) Error() string {
	return e.name + " is required"
}
