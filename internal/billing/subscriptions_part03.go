package billing

import (
	"errors"
	"os"
	"strings"

	"github.com/stripe/stripe-go/v82"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
)

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
