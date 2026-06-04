package db

import (
	"database/sql"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
)

const stripePurchaseStatusCompleted = "completed"

type StripePurchase struct {
	ID                      string
	UserID                  string
	LicenseID               string
	TierPurchased           Tier
	StripeCheckoutSessionID string
	StripePaymentIntentID   string
	StripeCustomerID        string
	StripeChargeID          string
	Amount                  int64
	Currency                string
	Status                  string
	EventSource             string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (db *Database) UpsertStripePurchase(purchase *StripePurchase) error {
	if purchase.ID == "" {
		purchase.ID = uuid.New().String()
	}

	_, err := db.Conn.Exec(`
		INSERT INTO stripe_purchases (
			id, user_id, license_id, tier_purchased, stripe_checkout_session_id,
			stripe_payment_intent_id, stripe_customer_id, stripe_charge_id,
			amount, currency, status, event_source
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (stripe_checkout_session_id) DO UPDATE SET
			license_id = EXCLUDED.license_id,
			tier_purchased = EXCLUDED.tier_purchased,
			stripe_payment_intent_id = EXCLUDED.stripe_payment_intent_id,
			stripe_customer_id = EXCLUDED.stripe_customer_id,
			stripe_charge_id = COALESCE(EXCLUDED.stripe_charge_id, stripe_purchases.stripe_charge_id),
			amount = EXCLUDED.amount,
			currency = EXCLUDED.currency,
			status = EXCLUDED.status,
			event_source = EXCLUDED.event_source,
			updated_at = NOW()
	`, purchase.ID, purchase.UserID, purchase.LicenseID, purchase.TierPurchased, purchase.StripeCheckoutSessionID,
		purchase.StripePaymentIntentID, purchase.StripeCustomerID, nullableString(purchase.StripeChargeID),
		purchase.Amount, purchase.Currency, purchase.Status, purchase.EventSource)
	if err != nil {
		log.Println("Failed to upsert Stripe purchase:", err)
	}
	return err
}

func (db *Database) GetStripePurchaseByPaymentIntent(paymentIntentID string) (*StripePurchase, error) {
	return db.getStripePurchaseByColumn("stripe_payment_intent_id", paymentIntentID)
}

func (db *Database) GetStripePurchaseByChargeID(chargeID string) (*StripePurchase, error) {
	return db.getStripePurchaseByColumn("stripe_charge_id", chargeID)
}

func (db *Database) UpdateStripePurchaseStatus(id, status, eventSource string) error {
	_, err := db.Conn.Exec(`
		UPDATE stripe_purchases
		SET status = $2,
			event_source = $3,
			updated_at = NOW()
		WHERE id = $1
	`, id, status, eventSource)
	if err != nil {
		log.Println("Failed to update Stripe purchase status:", err)
	}
	return err
}

func (db *Database) HasCompletedStripePurchase(userID string) (bool, error) {
	var exists bool

	err := db.Conn.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM stripe_purchases WHERE user_id = $1 AND status = $2)`,
		userID, stripePurchaseStatusCompleted,
	).Scan(&exists)
	if err != nil {
		log.Println("Failed to check Stripe purchase history:", err)
		return false, err
	}

	return exists, nil
}

func (db *Database) HasCompletedStripePurchaseForTier(userID string, tier Tier) (bool, error) {
	var exists bool

	err := db.Conn.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM stripe_purchases WHERE user_id = $1 AND tier_purchased = $2 AND status = $3)`,
		userID, tier, stripePurchaseStatusCompleted,
	).Scan(&exists)
	if err != nil {
		log.Println("Failed to check Stripe purchase history for tier:", err)
		return false, err
	}

	return exists, nil
}

func (db *Database) getStripePurchaseByColumn(column, value string) (*StripePurchase, error) {
	if value == "" {
		return nil, nil
	}

	query := `SELECT id, user_id, license_id, tier_purchased, stripe_checkout_session_id, stripe_payment_intent_id, stripe_customer_id, stripe_charge_id, amount, currency, status, event_source, created_at, updated_at
		FROM stripe_purchases WHERE ` + column + ` = $1`

	var purchase StripePurchase
	var paymentIntentID sql.NullString
	var customerID sql.NullString
	var chargeID sql.NullString
	err := db.Conn.QueryRow(query, value).Scan(
		&purchase.ID,
		&purchase.UserID,
		&purchase.LicenseID,
		&purchase.TierPurchased,
		&purchase.StripeCheckoutSessionID,
		&paymentIntentID,
		&customerID,
		&chargeID,
		&purchase.Amount,
		&purchase.Currency,
		&purchase.Status,
		&purchase.EventSource,
		&purchase.CreatedAt,
		&purchase.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		log.Println("Failed to get Stripe purchase:", err)
		return nil, err
	}

	purchase.StripePaymentIntentID = paymentIntentID.String
	purchase.StripeCustomerID = customerID.String
	purchase.StripeChargeID = chargeID.String
	return &purchase, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
