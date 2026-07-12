package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type InsufficientCreditsError struct {
	Required  int64
	Available int64
}

func (e InsufficientCreditsError) Error() string { return "insufficient credits" }

const (
	CreditMeterAssistantAI  = "assistant_ai"
	CreditMeterAutomationAI = "automation_ai"
	CreditRateCardVersion   = "2026-07-11-micro-v1"
	CreditDenominationScale = int64(1_000)
)

type CreditWallet struct {
	UserID             string    `json:"-"`
	MonthlyAllowance   int64     `json:"monthly_allowance"`
	MonthlyRemaining   int64     `json:"monthly_remaining"`
	PurchasedRemaining int64     `json:"purchased_remaining"`
	ReservedCredits    int64     `json:"reserved_credits"`
	AllowanceResetAt   time.Time `json:"allowance_reset_at"`
}

func (wallet CreditWallet) Available() int64 {
	available := wallet.MonthlyRemaining + wallet.PurchasedRemaining - wallet.ReservedCredits
	if available < 0 {
		return 0
	}
	return available
}

type CreditUsage struct {
	Provider          string
	Model             string
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
	Credits           int64
}

type CreditReservation struct {
	ID              string
	UserID          string
	ReservedCredits int64
}

type CreditUsageSummary struct {
	Meter   string `json:"meter"`
	Credits int64  `json:"credits"`
}

type CreditPurchase struct {
	ID                      string
	UserID                  string
	StripeCheckoutSessionID string
	StripePaymentIntentID   string
	PackID                  string
	Credits                 int64
	Status                  string
}

func MonthlyCreditAllowance(tier Tier) int64 {
	switch tier {
	case TierMax:
		return 6_000 * CreditDenominationScale
	case TierPro:
		return 2_000 * CreditDenominationScale
	default:
		return 100 * CreditDenominationScale
	}
}

func nextMonthlyReset(now time.Time) time.Time {
	return now.UTC().AddDate(0, 1, 0)
}

func (db *Database) GetOrCreateCreditWallet(userID string, tier Tier, now time.Time) (*CreditWallet, error) {
	allowance := MonthlyCreditAllowance(tier)
	now = now.UTC()
	var wallet CreditWallet
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `
			WITH released AS (
				UPDATE credit_reservations SET status = 'released', settled_at = NOW()
				WHERE user_id = $1 AND status = 'reserved' AND created_at < NOW() - INTERVAL '15 minutes'
				RETURNING reserved_credits
			)
			UPDATE credit_wallets SET reserved_credits = GREATEST(0, reserved_credits - COALESCE((SELECT SUM(reserved_credits) FROM released), 0))
			WHERE user_id = $1
		`, userID); err != nil {
			return err
		}
		result, err := tx.ExecContext(context.Background(), `
			INSERT INTO credit_wallets (user_id, monthly_allowance, monthly_remaining, allowance_reset_at)
			VALUES ($1, $2, $2, $3) ON CONFLICT (user_id) DO NOTHING
		`, userID, allowance, nextMonthlyReset(now))
		if err != nil {
			return err
		}
		inserted, _ := result.RowsAffected()
		var priorAllowance, priorRemaining int64
		var priorReset time.Time
		if err := tx.QueryRowContext(context.Background(), `SELECT monthly_allowance, monthly_remaining, allowance_reset_at FROM credit_wallets WHERE user_id = $1 FOR UPDATE`, userID).
			Scan(&priorAllowance, &priorRemaining, &priorReset); err != nil {
			return err
		}
		resetDue := inserted == 0 && !priorReset.After(now)
		nextReset := priorReset
		if resetDue {
			for !nextReset.After(now) {
				nextReset = nextReset.AddDate(0, 1, 0)
			}
		}
		if _, err := tx.ExecContext(context.Background(), `
			UPDATE credit_wallets
			SET monthly_allowance = $2,
			    monthly_remaining = CASE
			      WHEN allowance_reset_at <= $3 THEN $2
			      WHEN monthly_allowance <> $2 THEN GREATEST(0, $2 - GREATEST(0, monthly_allowance - monthly_remaining))
			      ELSE monthly_remaining
			    END,
			    allowance_reset_at = CASE WHEN allowance_reset_at <= $3 THEN $4 ELSE allowance_reset_at END,
			    updated_at = NOW()
			WHERE user_id = $1
		`, userID, allowance, now, nextReset); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `
			SELECT user_id, monthly_allowance, monthly_remaining, purchased_remaining,
			       reserved_credits, allowance_reset_at
			FROM credit_wallets WHERE user_id = $1
		`, userID).Scan(&wallet.UserID, &wallet.MonthlyAllowance, &wallet.MonthlyRemaining,
			&wallet.PurchasedRemaining, &wallet.ReservedCredits, &wallet.AllowanceResetAt); err != nil {
			return err
		}
		if inserted > 0 {
			return insertCreditLedgerEntry(tx, userID, "monthly_grant", allowance, 0, "monthly_grant:"+userID+":"+wallet.AllowanceResetAt.Format(time.RFC3339))
		}
		if resetDue {
			if priorRemaining > 0 {
				if err := insertCreditLedgerEntry(tx, userID, "expiration", -priorRemaining, 0, "monthly_expiration:"+userID+":"+priorReset.Format(time.RFC3339)); err != nil {
					return err
				}
			}
			return insertCreditLedgerEntry(tx, userID, "monthly_grant", allowance, 0, "monthly_grant:"+userID+":"+wallet.AllowanceResetAt.Format(time.RFC3339))
		}
		if wallet.MonthlyRemaining != priorRemaining {
			return insertCreditLedgerEntry(tx, userID, "adjustment", wallet.MonthlyRemaining-priorRemaining, 0, "plan_adjustment:"+userID+":"+now.Format(time.RFC3339Nano))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

func (db *Database) StartSubscriptionCreditPeriod(userID string, tier Tier, activatedAt time.Time, idempotencyKey string) (*CreditWallet, error) {
	if _, err := db.GetOrCreateCreditWallet(userID, tier, activatedAt); err != nil {
		return nil, err
	}
	allowance := MonthlyCreditAllowance(tier)
	var wallet CreditWallet
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		var alreadyStarted bool
		if err := tx.QueryRowContext(context.Background(), `
			SELECT EXISTS(SELECT 1 FROM credit_ledger WHERE idempotency_key = $1)
		`, "subscription_grant:"+idempotencyKey).Scan(&alreadyStarted); err != nil {
			return err
		}
		if alreadyStarted {
			return tx.QueryRowContext(context.Background(), `
				SELECT user_id, monthly_allowance, monthly_remaining, purchased_remaining,
				       reserved_credits, allowance_reset_at
				FROM credit_wallets WHERE user_id = $1
			`, userID).Scan(&wallet.UserID, &wallet.MonthlyAllowance, &wallet.MonthlyRemaining,
				&wallet.PurchasedRemaining, &wallet.ReservedCredits, &wallet.AllowanceResetAt)
		}
		var priorRemaining int64
		if err := tx.QueryRowContext(context.Background(), `SELECT monthly_remaining FROM credit_wallets WHERE user_id = $1 FOR UPDATE`, userID).Scan(&priorRemaining); err != nil {
			return err
		}
		if priorRemaining > 0 {
			if err := insertCreditLedgerEntry(tx, userID, "expiration", -priorRemaining, 0, "subscription_expiration:"+idempotencyKey); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(context.Background(), `
			UPDATE credit_wallets SET monthly_allowance = $2, monthly_remaining = $2,
			 allowance_reset_at = $3, updated_at = NOW() WHERE user_id = $1
		`, userID, allowance, nextMonthlyReset(activatedAt)); err != nil {
			return err
		}
		if err := insertCreditLedgerEntry(tx, userID, "monthly_grant", allowance, 0, "subscription_grant:"+idempotencyKey); err != nil {
			return err
		}
		return tx.QueryRowContext(context.Background(), `SELECT user_id, monthly_allowance, monthly_remaining, purchased_remaining, reserved_credits, allowance_reset_at FROM credit_wallets WHERE user_id = $1`, userID).
			Scan(&wallet.UserID, &wallet.MonthlyAllowance, &wallet.MonthlyRemaining, &wallet.PurchasedRemaining, &wallet.ReservedCredits, &wallet.AllowanceResetAt)
	})
	return &wallet, err
}

func insertCreditLedgerEntry(tx *sql.Tx, userID, source string, monthlyDelta, purchasedDelta int64, idempotencyKey string) error {
	_, err := tx.ExecContext(context.Background(), `
		INSERT INTO credit_ledger (id, user_id, source, monthly_delta, purchased_delta, idempotency_key)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (idempotency_key) DO NOTHING
	`, uuid.NewString(), userID, source, monthlyDelta, purchasedDelta, idempotencyKey)
	return err
}

func (db *Database) ReserveCredits(userID string, tier Tier, meter, idempotencyKey string, credits int64, now time.Time) (*CreditReservation, *CreditWallet, error) {
	if credits <= 0 || strings.TrimSpace(idempotencyKey) == "" {
		return nil, nil, errors.New("invalid credit reservation")
	}
	if _, err := db.GetOrCreateCreditWallet(userID, tier, now); err != nil {
		return nil, nil, err
	}
	reservation := &CreditReservation{ID: uuid.NewString(), UserID: userID, ReservedCredits: credits}
	var wallet CreditWallet
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(context.Background(), `
			SELECT user_id, monthly_allowance, monthly_remaining, purchased_remaining,
			       reserved_credits, allowance_reset_at
			FROM credit_wallets WHERE user_id = $1 FOR UPDATE
		`, userID).Scan(&wallet.UserID, &wallet.MonthlyAllowance, &wallet.MonthlyRemaining,
			&wallet.PurchasedRemaining, &wallet.ReservedCredits, &wallet.AllowanceResetAt); err != nil {
			return err
		}
		if wallet.Available() < credits {
			return InsufficientCreditsError{Available: wallet.Available(), Required: credits}
		}
		if _, err := tx.ExecContext(context.Background(), `
			INSERT INTO credit_reservations (id, user_id, idempotency_key, meter, reserved_credits)
			VALUES ($1,$2,$3,$4,$5)
		`, reservation.ID, userID, idempotencyKey, meter, credits); err != nil {
			return err
		}
		_, err := tx.ExecContext(context.Background(), `
			UPDATE credit_wallets SET reserved_credits = reserved_credits + $2, updated_at = NOW() WHERE user_id = $1
		`, userID, credits)
		return err
	})
	if err != nil {
		return nil, &wallet, err
	}
	wallet.ReservedCredits += credits
	return reservation, &wallet, nil
}

func (db *Database) ReleaseCreditReservation(reservationID string) error {
	return db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		var userID string
		var credits int64
		err := tx.QueryRowContext(context.Background(), `
			UPDATE credit_reservations SET status = 'released', settled_at = NOW()
			WHERE id = $1 AND status = 'reserved'
			RETURNING user_id, reserved_credits
		`, reservationID).Scan(&userID, &credits)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), `
			UPDATE credit_wallets SET reserved_credits = GREATEST(0, reserved_credits - $2), updated_at = NOW() WHERE user_id = $1
		`, userID, credits)
		return err
	})
}

func (db *Database) SettleCreditReservation(reservationID, idempotencyKey string, usage CreditUsage) (*CreditWallet, error) {
	if usage.Credits < 1 {
		usage.Credits = 1
	}
	var wallet CreditWallet
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		var userID, meter, status string
		var reserved int64
		if err := tx.QueryRowContext(context.Background(), `
			SELECT user_id, meter, reserved_credits, status FROM credit_reservations WHERE id = $1 FOR UPDATE
		`, reservationID).Scan(&userID, &meter, &reserved, &status); err != nil {
			return err
		}
		if status != "reserved" {
			return tx.QueryRowContext(context.Background(), `SELECT user_id, monthly_allowance, monthly_remaining, purchased_remaining, reserved_credits, allowance_reset_at FROM credit_wallets WHERE user_id = $1`, userID).
				Scan(&wallet.UserID, &wallet.MonthlyAllowance, &wallet.MonthlyRemaining, &wallet.PurchasedRemaining, &wallet.ReservedCredits, &wallet.AllowanceResetAt)
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT user_id, monthly_allowance, monthly_remaining, purchased_remaining, reserved_credits, allowance_reset_at FROM credit_wallets WHERE user_id = $1 FOR UPDATE`, userID).
			Scan(&wallet.UserID, &wallet.MonthlyAllowance, &wallet.MonthlyRemaining, &wallet.PurchasedRemaining, &wallet.ReservedCredits, &wallet.AllowanceResetAt); err != nil {
			return err
		}
		charge := usage.Credits
		if charge > reserved {
			charge = reserved
		}
		monthlyCharge := charge
		if monthlyCharge > wallet.MonthlyRemaining {
			monthlyCharge = wallet.MonthlyRemaining
		}
		purchasedCharge := charge - monthlyCharge
		if purchasedCharge > wallet.PurchasedRemaining {
			purchasedCharge = wallet.PurchasedRemaining
			charge = monthlyCharge + purchasedCharge
		}
		if _, err := tx.ExecContext(context.Background(), `
			UPDATE credit_wallets SET monthly_remaining = monthly_remaining - $2,
			 purchased_remaining = purchased_remaining - $3,
			 reserved_credits = GREATEST(0, reserved_credits - $4), updated_at = NOW() WHERE user_id = $1
		`, userID, monthlyCharge, purchasedCharge, reserved); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), `UPDATE credit_reservations SET status = 'settled', settled_at = NOW() WHERE id = $1`, reservationID); err != nil {
			return err
		}
		_, err := tx.ExecContext(context.Background(), `
			INSERT INTO credit_ledger (id, user_id, reservation_id, source, meter, monthly_delta,
			 purchased_delta, provider, model, input_tokens, cached_input_tokens, output_tokens,
			 reasoning_tokens, rate_card_version, credits_charged, idempotency_key)
			VALUES ($1,$2,$3,'consumption',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT (idempotency_key) DO NOTHING
		`, uuid.NewString(), userID, reservationID, meter, -monthlyCharge, -purchasedCharge,
			usage.Provider, usage.Model, usage.InputTokens, usage.CachedInputTokens,
			usage.OutputTokens, usage.ReasoningTokens, CreditRateCardVersion, charge, idempotencyKey)
		if err == nil {
			wallet.MonthlyRemaining -= monthlyCharge
			wallet.PurchasedRemaining -= purchasedCharge
			wallet.ReservedCredits -= reserved
		}
		return err
	})
	return &wallet, err
}

func (db *Database) AddPurchasedCredits(userID, packID, idempotencyKey string, credits int64) error {
	if credits <= 0 {
		return errors.New("credits must be positive")
	}
	return db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(context.Background(), `
			INSERT INTO credit_ledger (id, user_id, source, meter, purchased_delta, credits_charged, idempotency_key)
			VALUES ($1,$2,'purchase',$3,$4,0,$5) ON CONFLICT (idempotency_key) DO NOTHING
		`, uuid.NewString(), userID, packID, credits, idempotencyKey)
		if err != nil {
			return err
		}
		inserted, _ := result.RowsAffected()
		if inserted == 0 {
			return nil
		}
		_, err = tx.ExecContext(context.Background(), `UPDATE credit_wallets SET purchased_remaining = purchased_remaining + $2, updated_at = NOW() WHERE user_id = $1`, userID, credits)
		return err
	})
}

func (db *Database) RecordCreditPurchase(purchase CreditPurchase) error {
	if purchase.ID == "" {
		purchase.ID = uuid.NewString()
	}
	return db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(context.Background(), `
			INSERT INTO credit_purchases (id, user_id, stripe_checkout_session_id, stripe_payment_intent_id, pack_id, credits, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (stripe_checkout_session_id) DO NOTHING
		`, purchase.ID, purchase.UserID, purchase.StripeCheckoutSessionID, nullableString(purchase.StripePaymentIntentID), purchase.PackID, purchase.Credits, purchase.Status)
		return err
	})
}

func (db *Database) GetCreditPurchaseByPaymentIntent(paymentIntentID string) (*CreditPurchase, error) {
	if strings.TrimSpace(paymentIntentID) == "" {
		return nil, nil
	}
	var purchase CreditPurchase
	var paymentIntent sql.NullString
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(context.Background(), `
			SELECT id, user_id, stripe_checkout_session_id, stripe_payment_intent_id, pack_id, credits, status
			FROM credit_purchases WHERE stripe_payment_intent_id = $1
		`, paymentIntentID).Scan(&purchase.ID, &purchase.UserID, &purchase.StripeCheckoutSessionID, &paymentIntent, &purchase.PackID, &purchase.Credits, &purchase.Status)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	purchase.StripePaymentIntentID = paymentIntent.String
	return &purchase, nil
}

func (db *Database) RefundCreditPurchase(purchase *CreditPurchase) error {
	if purchase == nil || purchase.Status == "refunded" {
		return nil
	}
	return db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		var available int64
		if err := tx.QueryRowContext(context.Background(), `SELECT purchased_remaining FROM credit_wallets WHERE user_id = $1 FOR UPDATE`, purchase.UserID).Scan(&available); err != nil {
			return err
		}
		revoke := purchase.Credits
		if revoke > available {
			revoke = available
		}
		if _, err := tx.ExecContext(context.Background(), `UPDATE credit_wallets SET purchased_remaining = purchased_remaining - $2, updated_at = NOW() WHERE user_id = $1`, purchase.UserID, revoke); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), `UPDATE credit_purchases SET status = 'refunded', updated_at = NOW() WHERE id = $1`, purchase.ID); err != nil {
			return err
		}
		return insertCreditLedgerEntry(tx, purchase.UserID, "refund", 0, -revoke, "credit_refund:"+purchase.ID)
	})
}

func (db *Database) CreditUsageByMeter(userID string, since time.Time) ([]CreditUsageSummary, error) {
	var summaries []CreditUsageSummary
	err := db.withRLSContext(context.Background(), userRLSSettings(userID), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(context.Background(), `
			SELECT meter, COALESCE(SUM(credits_charged), 0) FROM credit_ledger
			WHERE user_id = $1 AND source = 'consumption' AND created_at >= $2 GROUP BY meter ORDER BY meter
		`, userID, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var summary CreditUsageSummary
			if err := rows.Scan(&summary.Meter, &summary.Credits); err != nil {
				return err
			}
			summaries = append(summaries, summary)
		}
		return rows.Err()
	})
	return summaries, err
}
