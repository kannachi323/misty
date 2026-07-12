package db

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	SubscriptionStatusTrialing = "trialing"
	SubscriptionStatusActive   = "active"
	SubscriptionStatusPastDue  = "past_due"
)

type StripeSubscription struct {
	ID                   string
	UserID               string
	LicenseID            string
	StripeSubscriptionID string
	StripeCustomerID     string
	StripePriceID        string
	Tier                 Tier
	BillingInterval      string
	Status               string
	CurrentPeriodEnd     *time.Time
	CancelAtPeriodEnd    bool
	CanceledAt           *time.Time
}

func (db *Database) UpsertStripeSubscription(subscription *StripeSubscription) error {
	if subscription.ID == "" {
		subscription.ID = uuid.NewString()
	}
	return db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `
			INSERT INTO stripe_subscriptions (
				id, user_id, license_id, stripe_subscription_id, stripe_customer_id,
				stripe_price_id, tier, billing_interval, status, current_period_end,
				cancel_at_period_end, canceled_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (stripe_subscription_id) DO UPDATE SET
				stripe_customer_id = EXCLUDED.stripe_customer_id,
				stripe_price_id = EXCLUDED.stripe_price_id,
				tier = EXCLUDED.tier,
				billing_interval = EXCLUDED.billing_interval,
				status = EXCLUDED.status,
				current_period_end = EXCLUDED.current_period_end,
				cancel_at_period_end = EXCLUDED.cancel_at_period_end,
				canceled_at = EXCLUDED.canceled_at,
				updated_at = NOW()
		`, subscription.ID, subscription.UserID, subscription.LicenseID,
			subscription.StripeSubscriptionID, subscription.StripeCustomerID,
			subscription.StripePriceID, subscription.Tier, subscription.BillingInterval,
			subscription.Status, subscription.CurrentPeriodEnd,
			subscription.CancelAtPeriodEnd, subscription.CanceledAt)
		return err
	})
}

func (db *Database) GetStripeSubscriptionByUserID(userID string) (*StripeSubscription, error) {
	return db.getStripeSubscription("user_id", userID, userRLSSettings(userID))
}

func (db *Database) GetStripeSubscriptionByStripeID(subscriptionID string) (*StripeSubscription, error) {
	return db.getStripeSubscription("stripe_subscription_id", subscriptionID, serviceRLSSettings())
}

func (db *Database) getStripeSubscription(column, value string, settings map[string]string) (*StripeSubscription, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var subscription StripeSubscription
	var periodEnd, canceledAt sql.NullTime
	err := db.withRLSContext(context.Background(), settings, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `
			SELECT id, user_id, license_id, stripe_subscription_id, stripe_customer_id,
			       stripe_price_id, tier, billing_interval, status, current_period_end,
			       cancel_at_period_end, canceled_at
			FROM stripe_subscriptions WHERE `+column+` = $1 ORDER BY updated_at DESC LIMIT 1
		`, value).Scan(&subscription.ID, &subscription.UserID, &subscription.LicenseID,
			&subscription.StripeSubscriptionID, &subscription.StripeCustomerID,
			&subscription.StripePriceID, &subscription.Tier, &subscription.BillingInterval,
			&subscription.Status, &periodEnd, &subscription.CancelAtPeriodEnd, &canceledAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if periodEnd.Valid {
		subscription.CurrentPeriodEnd = &periodEnd.Time
	}
	if canceledAt.Valid {
		subscription.CanceledAt = &canceledAt.Time
	}
	return &subscription, nil
}

func (db *Database) GetStripeCustomerIDForUser(userID string) (string, error) {
	subscription, err := db.GetStripeSubscriptionByUserID(userID)
	if err != nil {
		return "", err
	}
	if subscription != nil && subscription.StripeCustomerID != "" {
		return subscription.StripeCustomerID, nil
	}
	var customerID sql.NullString
	err = db.withRLSContext(context.Background(), userRLSSettings(userID), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `
			SELECT stripe_customer_id FROM stripe_purchases
			WHERE user_id = $1 AND stripe_customer_id IS NOT NULL
			ORDER BY updated_at DESC LIMIT 1
		`, userID).Scan(&customerID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return customerID.String, nil
}

func SubscriptionAllowsPaidAccess(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case SubscriptionStatusTrialing, SubscriptionStatusActive, SubscriptionStatusPastDue:
		return true
	default:
		return false
	}
}

func (db *Database) ApplyEffectiveSubscriptionEntitlement(subscription *StripeSubscription) error {
	if subscription == nil {
		return nil
	}
	tier := TierBasic
	if SubscriptionAllowsPaidAccess(subscription.Status) {
		tier = subscription.Tier
	} else {
		license, err := db.GetLicenseByUserID(subscription.UserID)
		if err != nil {
			return err
		}
		if license != nil && license.LegacyTier != nil {
			tier = *license.LegacyTier
		}
	}
	if err := db.SetLicenseStateByID(subscription.LicenseID, tier, LicenseStatusActive, nil); err != nil {
		log.Println("Failed to apply effective subscription entitlement:", err)
		return err
	}
	return nil
}

func (db *Database) StripeEventProcessed(eventID string) (bool, error) {
	var exists bool
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM stripe_webhook_events WHERE event_id = $1)`, eventID).Scan(&exists)
	})
	return exists, err
}

func (db *Database) MarkStripeEventProcessed(eventID, eventType string) error {
	return db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `
			INSERT INTO stripe_webhook_events (event_id, event_type) VALUES ($1, $2)
			ON CONFLICT (event_id) DO NOTHING
		`, eventID, eventType)
		return err
	})
}
