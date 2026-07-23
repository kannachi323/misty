package api

import (
	"bytes"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/kannachi323/misty/server/db"
	"github.com/kannachi323/misty/server/security"
)

const sessionCookieName = "misty_session"

func sessionUserID(r *http.Request, database *db.Database) (string, error) {
	token, ok := sessionTokenFromRequest(r)
	if !ok {
		return "", nil
	}
	tokenHash := security.HashToken(token)
	return database.GetSessionUserID(tokenHash)
}

func sessionTokenFromRequest(r *http.Request) (string, bool) {
	if token, ok := bearerTokenFromRequest(r); ok {
		return token, true
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	token := strings.TrimSpace(cookie.Value)
	return token, token != ""
}

func bearerTokenFromRequest(r *http.Request) (string, bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, ok := strings.Cut(authHeader, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func GetMe(database *db.Database) http.HandlerFunc {
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

		user, err := database.GetUserByID(userID)
		if err != nil || user == nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}

		license, err := database.GetLicenseByUserID(userID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if license == nil {
			http.Error(w, "license not found", http.StatusInternalServerError)
			return
		}

		subscription, err := database.GetStripeSubscriptionByUserID(userID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		billingKind := "free"
		if license.Status == db.LicenseStatusTrialing {
			billingKind = "trial"
		} else if subscription != nil && db.SubscriptionAllowsPaidAccess(subscription.Status) {
			billingKind = "subscription"
		} else if license.LegacyTier != nil {
			billingKind = "lifetime"
		}
		billingSummary := map[string]any{"kind": billingKind, "interval": nil, "subscription_status": nil,
			"current_period_end": nil, "cancel_at_period_end": false, "customer_portal_available": false}
		if subscription != nil {
			billingSummary["interval"] = subscription.BillingInterval
			billingSummary["subscription_status"] = subscription.Status
			billingSummary["current_period_end"] = subscription.CurrentPeriodEnd
			billingSummary["cancel_at_period_end"] = subscription.CancelAtPeriodEnd
			billingSummary["customer_portal_available"] = subscription.StripeCustomerID != ""
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":               user.ID,
			"name":             user.Name,
			"username":         user.Username,
			"email":            user.Email,
			"avatar_version":   user.AvatarVersion,
			"created_at":       user.CreatedAt,
			"tier":             string(db.NormalizePlan(license.Tier)),
			"status":           license.Status,
			"allows_use":       licenseAllowsUse(license),
			"expires_at":       license.ExpiresAt,
			"trial_started_at": license.TrialStartedAt,
			"license_device":   license.LicenseDevice,
			"billing":          billingSummary,
		})
	}
}

const maxAvatarPNGBytes = 5 << 20

func UserAvatar(database *db.Database) http.HandlerFunc {
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

		switch r.Method {
		case http.MethodGet:
			data, version, err := database.GetUserAvatar(userID)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if len(data) == 0 {
				http.Error(w, "avatar not found", http.StatusNotFound)
				return
			}
			writeAvatarPNG(w, data, version)
		case http.MethodPut:
			data, ok := readAvatarPNG(w, r)
			if !ok {
				return
			}
			version, err := database.UpdateUserAvatar(userID, data)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"avatar_version": version})
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func readAvatarPNG(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarPNGBytes+1)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) == 0 || len(data) > maxAvatarPNGBytes {
		http.Error(w, "PNG must be 5 MB or smaller", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		http.Error(w, "valid PNG required", http.StatusBadRequest)
		return nil, false
	}
	return data, true
}

func writeAvatarPNG(w http.ResponseWriter, data []byte, version int64) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("ETag", `"avatar-`+strconv.FormatInt(version, 10)+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func UpdateProfile(database *db.Database) http.HandlerFunc {
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
			Name string `json:"name"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}

		if err := database.UpdateUserName(userID, body.Name); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func UpdateDevice(database *db.Database) http.HandlerFunc {
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
			Device string `json:"device"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if err := database.UpdateLicenseDevice(userID, body.Device); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func GetSettings(database *db.Database) http.HandlerFunc {
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

		settings, err := database.GetUserSettingsByID(userID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if settings == nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"email_updates_enabled":   settings.EmailUpdatesEnabled,
			"analytics_enabled":       settings.AnalyticsEnabled,
			"error_reporting_enabled": settings.ErrorReportingEnabled,
		})
	}
}

func UpdateSettings(database *db.Database) http.HandlerFunc {
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
			EmailUpdatesEnabled   bool `json:"email_updates_enabled"`
			AnalyticsEnabled      bool `json:"analytics_enabled"`
			ErrorReportingEnabled bool `json:"error_reporting_enabled"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if err := database.UpdateUserSettings(userID, db.UserSettings{
			EmailUpdatesEnabled: body.EmailUpdatesEnabled, AnalyticsEnabled: body.AnalyticsEnabled, ErrorReportingEnabled: body.ErrorReportingEnabled,
		}); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func UpdateTelemetryPreferences(database *db.Database) http.HandlerFunc {
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
			AnalyticsEnabled      bool `json:"analytics_enabled"`
			ErrorReportingEnabled bool `json:"error_reporting_enabled"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := database.UpdateTelemetryPreferences(userID, body.AnalyticsEnabled, body.ErrorReportingEnabled); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
