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
	SourceEventCreatedAt *time.Time
	SourceEventID        string
	LastReconciledAt     *time.Time
	ReconcileAfter       time.Time
	ReconcileFailures    int
	LastReconcileError   string
}

func (db *Database) UpsertStripeSubscription(subscription *StripeSubscription) error {
	_, err := db.upsertStripeSubscription(subscription, false)
	return err
}

// UpsertStripeSubscriptionFromWebhook ignores an older Stripe event rather
// than allowing out-of-order delivery to roll back a newer subscription state.
func (db *Database) UpsertStripeSubscriptionFromWebhook(
	subscription *StripeSubscription,
) (bool, error) {
	return db.upsertStripeSubscription(subscription, true)
}

func (db *Database) upsertStripeSubscription(
	subscription *StripeSubscription,
	rejectOlder bool,
) (bool, error) {
	if subscription.ID == "" {
		subscription.ID = uuid.NewString()
	}
	if subscription.ReconcileAfter.IsZero() {
		subscription.ReconcileAfter = time.Now().UTC()
	}
	applied := false
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		query := `
			INSERT INTO stripe_subscriptions (
				id, user_id, license_id, stripe_subscription_id, stripe_customer_id,
				stripe_price_id, tier, billing_interval, status, current_period_end,
				cancel_at_period_end, canceled_at, source_event_created_at,
				source_event_id, reconcile_after
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (stripe_subscription_id) DO UPDATE SET
				stripe_customer_id = EXCLUDED.stripe_customer_id,
				stripe_price_id = EXCLUDED.stripe_price_id,
				tier = EXCLUDED.tier,
				billing_interval = EXCLUDED.billing_interval,
				status = EXCLUDED.status,
				current_period_end = EXCLUDED.current_period_end,
				cancel_at_period_end = EXCLUDED.cancel_at_period_end,
				canceled_at = EXCLUDED.canceled_at,
				source_event_created_at = COALESCE(
					EXCLUDED.source_event_created_at,
					stripe_subscriptions.source_event_created_at
				),
				source_event_id = CASE
					WHEN EXCLUDED.source_event_created_at IS NULL
						THEN stripe_subscriptions.source_event_id
					ELSE EXCLUDED.source_event_id
				END,
				reconcile_after = EXCLUDED.reconcile_after,
				updated_at = NOW()
		`
		if rejectOlder {
			query += `
				WHERE stripe_subscriptions.source_event_created_at IS NULL
				   OR EXCLUDED.source_event_created_at IS NULL
				   OR EXCLUDED.source_event_created_at > stripe_subscriptions.source_event_created_at
				   OR (
						EXCLUDED.source_event_created_at = stripe_subscriptions.source_event_created_at
						AND EXCLUDED.source_event_id <> stripe_subscriptions.source_event_id
						AND (
							stripe_subscriptions.status IN ('trialing','active')
							OR EXCLUDED.status NOT IN ('trialing','active')
						)
				   )
			`
		}
		result, err := tx.ExecContext(context.Background(), query,
			subscription.ID, subscription.UserID, subscription.LicenseID,
			subscription.StripeSubscriptionID, subscription.StripeCustomerID,
			subscription.StripePriceID, subscription.Tier, subscription.BillingInterval,
			subscription.Status, subscription.CurrentPeriodEnd,
			subscription.CancelAtPeriodEnd, subscription.CanceledAt,
			subscription.SourceEventCreatedAt, subscription.SourceEventID,
			subscription.ReconcileAfter)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		applied = rows == 1
		return err
	})
	return applied, err
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
	var periodEnd, canceledAt, sourceEventCreatedAt, lastReconciledAt sql.NullTime
	err := db.withRLSContext(context.Background(), settings, func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `
			SELECT id, user_id, license_id, stripe_subscription_id, stripe_customer_id,
			       stripe_price_id, tier, billing_interval, status, current_period_end,
			       cancel_at_period_end, canceled_at,source_event_created_at,
			       source_event_id,last_reconciled_at,reconcile_after,
			       reconcile_failures,last_reconcile_error
			FROM stripe_subscriptions WHERE `+column+` = $1 ORDER BY updated_at DESC LIMIT 1
		`, value).Scan(&subscription.ID, &subscription.UserID, &subscription.LicenseID,
			&subscription.StripeSubscriptionID, &subscription.StripeCustomerID,
			&subscription.StripePriceID, &subscription.Tier, &subscription.BillingInterval,
			&subscription.Status, &periodEnd, &subscription.CancelAtPeriodEnd, &canceledAt,
			&sourceEventCreatedAt, &subscription.SourceEventID, &lastReconciledAt,
			&subscription.ReconcileAfter, &subscription.ReconcileFailures,
			&subscription.LastReconcileError)
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
	if sourceEventCreatedAt.Valid {
		subscription.SourceEventCreatedAt = &sourceEventCreatedAt.Time
	}
	if lastReconciledAt.Valid {
		subscription.LastReconciledAt = &lastReconciledAt.Time
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
	case SubscriptionStatusTrialing, SubscriptionStatusActive:
		return true
	default:
		return false
	}
}

func (db *Database) ApplyEffectiveSubscriptionEntitlement(subscription *StripeSubscription) error {
	if subscription == nil {
		return nil
	}
	if strings.EqualFold(subscription.Status, SubscriptionStatusTrialing) {
		return db.SetStripeTrialState(subscription.LicenseID, subscription.Tier, subscription.CurrentPeriodEnd)
	}
	tier := TierBasic
	if SubscriptionAllowsPaidAccess(subscription.Status) {
		tier = NormalizePlan(subscription.Tier)
	} else {
		license, err := db.GetLicenseByUserID(subscription.UserID)
		if err != nil {
			return err
		}
		if license != nil && license.LegacyTier != nil {
			tier = NormalizePlan(*license.LegacyTier)
		}
	}
	if err := db.SetLicenseStateByID(subscription.LicenseID, tier, LicenseStatusActive, nil); err != nil {
		log.Println("Failed to apply effective subscription entitlement:", err)
		return err
	}
	return nil
}

func (db *Database) ListStripeSubscriptionsDueForReconciliation(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]StripeSubscription, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	var subscriptions []StripeSubscription
	err := db.withRLSContext(ctx, serviceRLSSettings(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id,user_id,license_id,stripe_subscription_id,stripe_customer_id,
			       stripe_price_id,tier,billing_interval,status,current_period_end,
			       cancel_at_period_end,canceled_at,source_event_created_at,
			       source_event_id,last_reconciled_at,reconcile_after,
			       reconcile_failures,last_reconcile_error
			FROM stripe_subscriptions
			WHERE status IN ('trialing','active','past_due')
			  AND reconcile_after<=$1
			ORDER BY reconcile_after,id
			LIMIT $2
		`, now.UTC(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var subscription StripeSubscription
			var periodEnd, canceledAt, sourceCreated, reconciled sql.NullTime
			if err := rows.Scan(
				&subscription.ID,
				&subscription.UserID,
				&subscription.LicenseID,
				&subscription.StripeSubscriptionID,
				&subscription.StripeCustomerID,
				&subscription.StripePriceID,
				&subscription.Tier,
				&subscription.BillingInterval,
				&subscription.Status,
				&periodEnd,
				&subscription.CancelAtPeriodEnd,
				&canceledAt,
				&sourceCreated,
				&subscription.SourceEventID,
				&reconciled,
				&subscription.ReconcileAfter,
				&subscription.ReconcileFailures,
				&subscription.LastReconcileError,
			); err != nil {
				return err
			}
			if periodEnd.Valid {
				subscription.CurrentPeriodEnd = &periodEnd.Time
			}
			if canceledAt.Valid {
				subscription.CanceledAt = &canceledAt.Time
			}
			if sourceCreated.Valid {
				subscription.SourceEventCreatedAt = &sourceCreated.Time
			}
			if reconciled.Valid {
				subscription.LastReconciledAt = &reconciled.Time
			}
			subscriptions = append(subscriptions, subscription)
		}
		return rows.Err()
	})
	return subscriptions, err
}

func (db *Database) MarkStripeSubscriptionReconciled(
	ctx context.Context,
	subscriptionID string,
	now, reconcileAfter time.Time,
) error {
	return db.withRLSContext(ctx, serviceRLSSettings(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE stripe_subscriptions
			SET last_reconciled_at=$2,reconcile_after=$3,reconcile_failures=0,
			    last_reconcile_error='',updated_at=NOW()
			WHERE stripe_subscription_id=$1
		`, subscriptionID, now.UTC(), reconcileAfter.UTC())
		return err
	})
}

func (db *Database) MarkStripeSubscriptionReconcileFailed(
	ctx context.Context,
	subscriptionID, failure string,
	reconcileAfter time.Time,
) error {
	if len(failure) > 500 {
		failure = failure[:500]
	}
	return db.withRLSContext(ctx, serviceRLSSettings(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE stripe_subscriptions
			SET reconcile_after=$2,reconcile_failures=reconcile_failures+1,
			    last_reconcile_error=$3,updated_at=NOW()
			WHERE stripe_subscription_id=$1
		`, subscriptionID, reconcileAfter.UTC(), failure)
		return err
	})
}

// ExpireStaleSubscriptionEntitlements is the bounded fail-safe for a missed
// webhook plus repeated Stripe API failures. It does not mutate the Stripe
// status, so checkout remains blocked and a later successful reconciliation
// can restore the canonical paid tier without creating another subscription.
func (db *Database) ExpireStaleSubscriptionEntitlements(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	affected := int64(0)
	err := db.withRLSContext(ctx, serviceRLSSettings(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			WITH stale AS (
				SELECT s.license_id
				FROM stripe_subscriptions s
				WHERE s.status IN ('trialing','active')
				  AND s.current_period_end IS NOT NULL
				  AND s.current_period_end<=$1
				ORDER BY s.current_period_end,s.id
				LIMIT $2
			)
			UPDATE licenses l
			SET tier=COALESCE(NULLIF(l.legacy_tier,''),'basic'),
			    status='active',expires_at=NULL,updated_at=NOW()
			FROM stale
			WHERE l.id=stale.license_id
			  AND l.tier IN ('pro','max','personal')
		`, cutoff.UTC(), limit)
		if err != nil {
			return err
		}
		affected, err = result.RowsAffected()
		return err
	})
	return int(affected), err
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
