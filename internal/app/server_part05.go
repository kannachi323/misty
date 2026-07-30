package app

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
)

func TestingPasswordResetRedirectURLFromEnv() (string, error) {
	rawURL := os.Getenv("PASSWORD_RESET_URL")
	if rawURL == "" {
		rawURL = "http://localhost:5173/#/reset"
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid PASSWORD_RESET_URL: %w", err)
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("PASSWORD_RESET_URL must include a host")
	}
	if parsedURL.Scheme == "https" {
		return parsedURL.String(), nil
	}
	if parsedURL.Scheme == "http" && TestingIsLocalhostHostname(parsedURL.Hostname()) {
		return parsedURL.String(), nil
	}

	return "", fmt.Errorf("PASSWORD_RESET_URL must use https unless it targets localhost")
}

func TestingPasswordResetStartURLFromEnv() (string, error) {
	rawURL := os.Getenv("PASSWORD_RESET_START_URL")
	if rawURL == "" {
		rawURL = "http://localhost:8080/auth/reset/start"
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid PASSWORD_RESET_START_URL: %w", err)
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("PASSWORD_RESET_START_URL must include a host")
	}
	if parsedURL.Scheme == "https" {
		return parsedURL.String(), nil
	}
	if parsedURL.Scheme == "http" && TestingIsLocalhostHostname(parsedURL.Hostname()) {
		return parsedURL.String(), nil
	}

	return "", fmt.Errorf("PASSWORD_RESET_START_URL must use https unless it targets localhost")
}

func TestingIsLocalhostHostname(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

const TestingDefaultStripeWebhookPath = "/stripe/webhook"

func TestingStripeWebhookPathFromEnv() (string, error) {
	rawPath := strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_PATH"))
	if rawPath == "" {
		return TestingDefaultStripeWebhookPath, nil
	}

	parsedPath, err := url.ParseRequestURI(rawPath)
	if err != nil ||
		!strings.HasPrefix(rawPath, "/") ||
		strings.HasPrefix(rawPath, "//") ||
		parsedPath.IsAbs() ||
		parsedPath.Host != "" ||
		parsedPath.RawQuery != "" ||
		parsedPath.Fragment != "" ||
		rawPath == "/" ||
		strings.ContainsAny(rawPath, "{}*") {
		return "", fmt.Errorf("STRIPE_WEBHOOK_PATH must be a static absolute path such as %s", TestingDefaultStripeWebhookPath)
	}

	return rawPath, nil
}

// warnOnInsecureBillingConfiguration surfaces a missing webhook secret at boot
// rather than on the first forged event. The handler refuses those requests
// regardless; this exists so the operator learns before real Stripe events are
// silently rejected too.
func warnOnInsecureBillingConfiguration() {
	if len(strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET"))) < 16 {
		log.Println("SECURITY: STRIPE_WEBHOOK_SECRET is unset or too short; Stripe webhooks will be refused")
	}
	if trustProxyHeadersEnabled() && strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS")) == "" {
		log.Println("NOTICE: TRUST_PROXY_HEADERS is on and TRUSTED_PROXY_CIDRS is unset; " +
			"forwarded headers are honoured only from loopback and private ranges")
	}
}

func trustProxyHeadersEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TRUST_PROXY_HEADERS"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
