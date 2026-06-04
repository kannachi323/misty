package billing

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/kannachi323/misty/server/db"
	"github.com/stripe/stripe-go/v82"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	couponapi "github.com/stripe/stripe-go/v82/coupon"
	priceapi "github.com/stripe/stripe-go/v82/price"
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

type checkoutSessionOptions struct {
	discountCouponID string
}

type CheckoutSessionCreator func(cfg checkoutConfig, user *db.User, tier db.Tier, opts checkoutSessionOptions) (string, error)
type StripePriceFetcher func(secretKey string, priceID string) (*stripe.Price, error)
type StripeCouponCreator func(secretKey string, amountOff int64, currency string) (string, error)

type Service struct {
	database              *db.Database
	createCheckoutSession CheckoutSessionCreator
	fetchPrice            StripePriceFetcher
	createCoupon          StripeCouponCreator
	trialDuration         time.Duration
}

type ServiceOption func(*Service)

func NewService(database *db.Database, opts ...ServiceOption) *Service {
	service := &Service{
		database:              database,
		createCheckoutSession: createStripeCheckoutSession,
		fetchPrice:            fetchStripePrice,
		createCoupon:          createStripeCoupon,
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

func WithStripePriceFetcher(fn StripePriceFetcher) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.fetchPrice = fn
		}
	}
}

func WithStripeCouponCreator(fn StripeCouponCreator) ServiceOption {
	return func(service *Service) {
		if fn != nil {
			service.createCoupon = fn
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

	opts := checkoutSessionOptions{}
	if tier == db.TierPro {
		eligible, err := service.IsProUpgradeDiscountEligible(userID)
		if err != nil {
			return "", err
		}
		if eligible {
			couponID, err := service.createProUpgradeCoupon(config)
			if err != nil {
				return "", err
			}
			opts.discountCouponID = couponID
		}
	}

	return service.createCheckoutSession(config, user, tier, opts)
}

func (service *Service) IsProUpgradeDiscountEligible(userID string) (bool, error) {
	license, err := service.database.GetLicenseByUserID(userID)
	if err != nil {
		return false, err
	}
	if license == nil {
		return false, ErrLicenseNotFound
	}

	hasPersonalPurchase, err := service.database.HasCompletedStripePurchaseForTier(userID, db.TierPersonal)
	if err != nil {
		return false, err
	}

	return shouldApplyProUpgradeDiscount(license, hasPersonalPurchase, db.TierPro), nil
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

func createStripeCheckoutSession(cfg checkoutConfig, user *db.User, tier db.Tier, opts checkoutSessionOptions) (string, error) {
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
	if opts.discountCouponID != "" {
		params.Discounts = []*stripe.CheckoutSessionDiscountParams{
			{Coupon: stripe.String(opts.discountCouponID)},
		}
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

func (service *Service) createProUpgradeCoupon(cfg checkoutConfig) (string, error) {
	personalPrice, err := service.fetchPrice(cfg.secretKey, cfg.prices[db.TierPersonal])
	if err != nil {
		return "", err
	}

	amountOff, currency, err := computeUpgradeDiscount(personalPrice)
	if err != nil {
		return "", err
	}

	return service.createCoupon(cfg.secretKey, amountOff, currency)
}

func shouldApplyProUpgradeDiscount(license *db.License, hasCompletedPersonalPurchase bool, requestedTier db.Tier) bool {
	return requestedTier == db.TierPro &&
		license != nil &&
		license.Tier == db.TierPersonal &&
		license.Status == db.LicenseStatusActive &&
		hasCompletedPersonalPurchase
}

func computeUpgradeDiscount(personalPrice *stripe.Price) (int64, string, error) {
	if personalPrice == nil {
		return 0, "", errors.New("missing stripe price")
	}
	if personalPrice.Currency == "" {
		return 0, "", errors.New("stripe price currency is required")
	}
	if personalPrice.UnitAmount <= 0 {
		return 0, "", errors.New("stripe price amount is required")
	}

	return personalPrice.UnitAmount, strings.ToLower(string(personalPrice.Currency)), nil
}

func fetchStripePrice(secretKey string, priceID string) (*stripe.Price, error) {
	if strings.TrimSpace(secretKey) == "" || strings.TrimSpace(priceID) == "" {
		return nil, errors.New("stripe secret key and price id are required")
	}

	stripe.Key = secretKey
	return priceapi.Get(priceID, nil)
}

func createStripeCoupon(secretKey string, amountOff int64, currency string) (string, error) {
	if strings.TrimSpace(secretKey) == "" || amountOff <= 0 || strings.TrimSpace(currency) == "" {
		return "", errors.New("stripe coupon config is invalid")
	}

	stripe.Key = secretKey
	coupon, err := couponapi.New(&stripe.CouponParams{
		AmountOff: stripe.Int64(amountOff),
		Currency:  stripe.String(strings.ToLower(strings.TrimSpace(currency))),
		Duration:  stripe.String(string(stripe.CouponDurationOnce)),
		Name:      stripe.String("upgrade to pro"),
	})
	if err != nil {
		return "", err
	}
	return coupon.ID, nil
}

type configError struct {
	name string
}

func (e *configError) Error() string {
	return e.name + " is required"
}
