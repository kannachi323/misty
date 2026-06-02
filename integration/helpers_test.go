package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/joho/godotenv"
	"github.com/kannachi323/misty/server/billing"
	"github.com/kannachi323/misty/server/db"
)

const sessionCookieName = "misty_session"
const testDatabaseLockID int64 = 621042

var loadTestEnvOnce sync.Once

func loadTestEnv() {
	loadTestEnvOnce.Do(func() {
		_ = godotenv.Load()
	})
}

type integrationDBConfig struct {
	host     string
	port     string
	user     string
	password string
	name     string
	sslmode  string
}

func openIntegrationDatabase(t *testing.T) *db.Database {
	t.Helper()

	loadTestEnv()
	cfg := loadIntegrationDBConfig(t)

	conn, err := sql.Open("postgres", cfg.dsn())
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		t.Fatalf("database ping failed: %v", err)
	}

	database := &db.Database{Conn: conn}
	acquireTestDatabaseLock(t, database)
	resetIntegrationDatabase(t, database)

	t.Cleanup(func() {
		resetIntegrationDatabase(t, database)
		releaseTestDatabaseLock(t, database)
		database.Stop()
	})

	return database
}

func loadIntegrationDBConfig(t *testing.T) integrationDBConfig {
	t.Helper()

	useTestPrefix := false
	for _, key := range []string{"HOST", "PORT", "USER", "PASSWORD", "NAME", "SSLMODE"} {
		if strings.TrimSpace(os.Getenv("TEST_DB_"+key)) != "" {
			useTestPrefix = true
			break
		}
	}

	read := func(key string) string {
		if useTestPrefix {
			return strings.TrimSpace(os.Getenv("TEST_DB_" + key))
		}
		return strings.TrimSpace(os.Getenv("DB_" + key))
	}

	cfg := integrationDBConfig{
		host:     read("HOST"),
		port:     read("PORT"),
		user:     read("USER"),
		password: read("PASSWORD"),
		name:     read("NAME"),
		sslmode:  read("SSLMODE"),
	}

	if cfg.port == "" {
		cfg.port = "5432"
	}
	if cfg.sslmode == "" {
		cfg.sslmode = "disable"
	}

	switch {
	case cfg.host == "":
		t.Fatal("missing integration DB host; set TEST_DB_HOST or DB_HOST")
	case cfg.user == "":
		t.Fatal("missing integration DB user; set TEST_DB_USER or DB_USER")
	case cfg.password == "":
		t.Fatal("missing integration DB password; set TEST_DB_PASSWORD or DB_PASSWORD")
	case cfg.name == "":
		t.Fatal("missing integration DB name; set TEST_DB_NAME or DB_NAME")
	}

	if !strings.Contains(strings.ToLower(strings.TrimSpace(cfg.name)), "test") {
		t.Fatalf("refusing to reset non-test database %q; configure TEST_DB_* or use a DB name containing \"test\"", cfg.name)
	}

	return cfg
}

func (cfg integrationDBConfig) dsn() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.host, cfg.port, cfg.user, cfg.password, cfg.name, cfg.sslmode,
	)
}

func resetIntegrationDatabase(t *testing.T, database *db.Database) {
	t.Helper()

	_, err := database.Conn.Exec(`
		TRUNCATE TABLE
			stripe_purchases,
			sessions,
			password_reset_tokens,
			waitlist_signups,
			users,
			licenses
		RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("failed to reset integration database: %v", err)
	}
}

func acquireTestDatabaseLock(t *testing.T, database *db.Database) {
	t.Helper()

	if _, err := database.Conn.Exec(`SELECT pg_advisory_lock($1)`, testDatabaseLockID); err != nil {
		t.Fatalf("failed to acquire test database lock: %v", err)
	}
}

func releaseTestDatabaseLock(t *testing.T, database *db.Database) {
	t.Helper()

	if _, err := database.Conn.Exec(`SELECT pg_advisory_unlock($1)`, testDatabaseLockID); err != nil {
		t.Fatalf("failed to release test database lock: %v", err)
	}
}

func performJSONRequest(t *testing.T, handler http.HandlerFunc, method string, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("json.NewEncoder() error = %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &payload)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeJSONResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %q", err, rec.Body.String())
	}
	return payload
}

func requireCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()

	resp := rec.Result()
	defer resp.Body.Close()

	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}

	t.Fatalf("cookie %q not found", name)
	return nil
}

func countStripePurchases(t *testing.T, database *db.Database, checkoutSessionID string) int {
	t.Helper()

	var count int
	err := database.Conn.QueryRow(
		`SELECT COUNT(*) FROM stripe_purchases WHERE stripe_checkout_session_id = $1`,
		checkoutSessionID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count stripe purchases: %v", err)
	}
	return count
}

func totalStripePurchases(t *testing.T, database *db.Database) int {
	t.Helper()

	var count int
	err := database.Conn.QueryRow(`SELECT COUNT(*) FROM stripe_purchases`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count stripe purchases: %v", err)
	}
	return count
}

func newTestStripeService(database *db.Database) *billing.StripeService {
	return billing.NewStripeService(database, billing.WithChargeIDFetcher(func(paymentIntentID string) (string, error) {
		return "ch_" + paymentIntentID, nil
	}))
}

type passwordResetEmailCall struct {
	recipientEmail string
	resetLink      string
}

type fakePasswordResetSender struct {
	calls []passwordResetEmailCall
	err   error
}

func (s *fakePasswordResetSender) SendPasswordResetEmail(_ context.Context, recipientEmail, resetLink string) error {
	s.calls = append(s.calls, passwordResetEmailCall{
		recipientEmail: recipientEmail,
		resetLink:      resetLink,
	})
	return s.err
}

type waitlistEmailCall struct {
	recipientName  string
	recipientEmail string
	notifyEmail    string
	waitlistName   string
	waitlistEmail  string
}

type fakeWaitlistSender struct {
	confirmationCalls []waitlistEmailCall
	notificationCalls []waitlistEmailCall
	confirmationErr   error
	notificationErr   error
}

func (s *fakeWaitlistSender) SendWaitlistConfirmationEmail(_ context.Context, recipientName, recipientEmail string) error {
	s.confirmationCalls = append(s.confirmationCalls, waitlistEmailCall{
		recipientName:  recipientName,
		recipientEmail: recipientEmail,
	})
	return s.confirmationErr
}

func (s *fakeWaitlistSender) SendWaitlistNotificationEmail(_ context.Context, notifyEmail, waitlistName, waitlistEmail string) error {
	s.notificationCalls = append(s.notificationCalls, waitlistEmailCall{
		notifyEmail:   notifyEmail,
		waitlistName:  waitlistName,
		waitlistEmail: waitlistEmail,
	})
	return s.notificationErr
}

func countWaitlistSignups(t *testing.T, database *db.Database) int {
	t.Helper()

	var count int
	err := database.Conn.QueryRow(`SELECT COUNT(*) FROM waitlist_signups`).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count waitlist signups: %v", err)
	}
	return count
}

func getStoredPasswordResetTokenHash(t *testing.T, database *db.Database, userID string) string {
	t.Helper()

	var tokenHash string
	err := database.Conn.QueryRow(
		`SELECT hashed_token FROM password_reset_tokens WHERE user_id = $1`,
		userID,
	).Scan(&tokenHash)
	if err != nil {
		t.Fatalf("failed to fetch password reset token hash: %v", err)
	}
	return tokenHash
}
