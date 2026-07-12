package api

import (
	"errors"
	"net/http"
	"time"

	appbilling "github.com/kannachi323/misty/server/billing"
	"github.com/kannachi323/misty/server/db"
)

func CreateCheckoutSession(database *db.Database) http.HandlerFunc {
	service := appbilling.NewService(database)

	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := sessionUserID(r, database)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if userID == "" {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}

		var body struct {
			Tier     db.Tier                    `json:"tier"`
			Interval appbilling.BillingInterval `json:"interval"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		url, err := service.CreateCheckoutSession(userID, body.Tier, body.Interval)
		switch {
		case errors.Is(err, appbilling.ErrInvalidTier):
			http.Error(w, "invalid tier", http.StatusBadRequest)
			return
		case errors.Is(err, appbilling.ErrInvalidInterval):
			http.Error(w, "invalid billing interval", http.StatusBadRequest)
			return
		case errors.Is(err, appbilling.ErrSubscriptionExists):
			http.Error(w, "active subscription already exists", http.StatusConflict)
			return
		case errors.Is(err, appbilling.ErrUserNotFound):
			http.Error(w, "user not found", http.StatusNotFound)
			return
		case err != nil:
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"url": url})
	}
}

func CreateCreditCheckoutSession(database *db.Database) http.HandlerFunc {
	service := appbilling.NewService(database)
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := sessionUserID(r, database)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if userID == "" {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		var body struct {
			PackID string `json:"pack_id"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		url, err := service.CreateCreditCheckoutSession(userID, body.PackID)
		switch {
		case errors.Is(err, appbilling.ErrInvalidCreditPack):
			http.Error(w, "invalid credit pack", http.StatusBadRequest)
		case err != nil:
			http.Error(w, "internal error", http.StatusInternalServerError)
		default:
			writeJSON(w, http.StatusOK, map[string]string{"url": url})
		}
	}
}

func CreatePortalSession(database *db.Database) http.HandlerFunc {
	service := appbilling.NewService(database)
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := sessionUserID(r, database)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if userID == "" {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		url, err := service.CreatePortalSession(userID)
		if errors.Is(err, appbilling.ErrPortalUnavailable) {
			http.Error(w, "customer portal unavailable", http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": url})
	}
}

func GetBillingUsage(database *db.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := sessionUserID(r, database)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if userID == "" {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		license, err := database.GetLicenseByUserID(userID)
		if err != nil || license == nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		wallet, err := database.GetOrCreateCreditWallet(userID, license.Tier, time.Now())
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		usage, err := database.CreditUsageByMeter(userID, wallet.AllowanceResetAt.AddDate(0, -1, 0))
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": license.Tier, "monthly_allowance": wallet.MonthlyAllowance,
			"monthly_remaining": wallet.MonthlyRemaining, "purchased_remaining": wallet.PurchasedRemaining,
			"available_credits": wallet.Available(), "reserved_credits": wallet.ReservedCredits,
			"next_reset_at": wallet.AllowanceResetAt, "usage_by_meter": usage})
	}
}

func StartPersonalTrial(database *db.Database) http.HandlerFunc {
	service := appbilling.NewService(database)

	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := sessionUserID(r, database)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if userID == "" {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}

		updatedLicense, err := service.StartProTrial(userID)
		switch {
		case errors.Is(err, appbilling.ErrLicenseNotFound):
			http.Error(w, "license not found", http.StatusInternalServerError)
			return
		case errors.Is(err, appbilling.ErrTrialUnavailable):
			http.Error(w, "trial unavailable", http.StatusConflict)
			return
		case err != nil:
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"tier":       string(updatedLicense.Tier),
			"status":     updatedLicense.Status,
			"expires_at": updatedLicense.ExpiresAt,
		})
	}
}
