package api

import (
	"io"
	"log"
	"net/http"
	"os"

	appbilling "github.com/kannachi323/misty/server/billing"
	"github.com/kannachi323/misty/server/db"
	"github.com/stripe/stripe-go/v82/webhook"
)

func StripeWebhook(database *db.Database) http.HandlerFunc {
	return StripeWebhookWithService(os.Getenv("STRIPE_WEBHOOK_SECRET"), appbilling.NewStripeService(database))
}

func StripeWebhookWithService(webhookSecret string, service *appbilling.StripeService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		event, err := webhook.ConstructEventWithOptions(
			body,
			r.Header.Get("Stripe-Signature"),
			webhookSecret,
			webhook.ConstructEventOptions{
				IgnoreAPIVersionMismatch: true,
			},
		)
		if err != nil {
			log.Println("Stripe signature verification failed:", err)
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}

		service.HandleWebhookEvent(string(event.Type), event.Data.Raw)

		w.WriteHeader(http.StatusOK)
	}
}
