package api

import (
	"net/http"
	"strings"

	appbilling "github.com/kannachi323/misty/server/billing"
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

		proUpgradeDiscountEligible, err := appbilling.NewService(database).IsProUpgradeDiscountEligible(userID)
		if err != nil && err != appbilling.ErrLicenseNotFound {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"id":                            user.ID,
			"name":                          user.Name,
			"email":                         user.Email,
			"created_at":                    user.CreatedAt,
			"tier":                          string(license.Tier),
			"status":                        license.Status,
			"allows_use":                    licenseAllowsUse(license),
			"expires_at":                    license.ExpiresAt,
			"trial_started_at":              license.TrialStartedAt,
			"license_device":                license.LicenseDevice,
			"pro_upgrade_discount_eligible": proUpgradeDiscountEligible,
		})
	}
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
			"email_updates_enabled": settings.EmailUpdatesEnabled,
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
			EmailUpdatesEnabled bool `json:"email_updates_enabled"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if err := database.UpdateUserSettings(userID, db.UserSettings{
			EmailUpdatesEnabled: body.EmailUpdatesEnabled,
		}); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
