package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type HostedAILimitReachedError struct {
	Required  int64
	Available int64
}

func (e HostedAILimitReachedError) Error() string { return "hosted AI weekly limit reached" }

// InsufficientCreditsError remains as a source-compatibility alias while older
// clients roll off. It is never serialized to customers.
type InsufficientCreditsError = HostedAILimitReachedError

const (
	HostedAIMeterAssistant     = "assistant_ai"
	HostedAIMeterAutomation    = "automation_ai"
	HostedAIMeterSemanticQuery = "semantic_query"
	HostedAIRateCardVersion    = "2026-07-22-weekly-microusd-v1"
	MicrousdPerDollar          = int64(1_000_000)

	CreditMeterAssistantAI  = HostedAIMeterAssistant
	CreditMeterAutomationAI = HostedAIMeterAutomation
	CreditRateCardVersion   = HostedAIRateCardVersion
	CreditDenominationScale = int64(1_000)
)

type HostedAIWallet struct {
	UserID                  string    `json:"-"`
	WeeklyAllowanceMicrousd int64     `json:"-"`
	WeeklyRemainingMicrousd int64     `json:"-"`
	ReservedMicrousd        int64     `json:"-"`
	ResetAt                 time.Time `json:"reset_at"`
}

func (wallet HostedAIWallet) Available() int64 {
	available := wallet.WeeklyRemainingMicrousd - wallet.ReservedMicrousd
	if available < 0 {
		return 0
	}
	return available
}

func (wallet HostedAIWallet) UsedRatio() float64 {
	if wallet.WeeklyAllowanceMicrousd <= 0 {
		return 1
	}
	used := wallet.WeeklyAllowanceMicrousd - wallet.WeeklyRemainingMicrousd
	if used <= 0 {
		return 0
	}
	if used >= wallet.WeeklyAllowanceMicrousd {
		return 1
	}
	return float64(used) / float64(wallet.WeeklyAllowanceMicrousd)
}

type CreditWallet = HostedAIWallet

type HostedAIUsage struct {
	Provider          string
	Model             string
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	ReasoningTokens   int64
	ProviderCost      int64
	ChargeMicrousd    int64
	// Credits is accepted only by compatibility callers and means micro-USD.
	Credits int64
}

type CreditUsage = HostedAIUsage

type HostedAIReservation struct {
	ID               string
	UserID           string
	ReservedMicrousd int64
	ReservedCredits  int64
	Status           string
}

type CreditReservation = HostedAIReservation

type HostedAIUsageSummary struct {
	Meter           string `json:"meter"`
	ChargedMicrousd int64  `json:"-"`
}

type CreditUsageSummary = HostedAIUsageSummary

// CreditPurchase is retained only so legacy webhook code can compile during
// the compatibility window. New purchases are permanently disabled.
type CreditPurchase struct {
	ID, UserID, StripeCheckoutSessionID, StripePaymentIntentID, PackID, Status string
	Credits                                                                    int64
}

func WeeklyHostedAIAllowance(tier Tier) int64 {
	return EntitlementsForTier(tier).WeeklyHostedAIAllowance
}

func MonthlyCreditAllowance(tier Tier) int64 { return WeeklyHostedAIAllowance(tier) }

func nextWeeklyReset(now time.Time) time.Time {
	now = now.UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	days := (8 - int(midnight.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	return midnight.AddDate(0, 0, days)
}

func (db *Database) GetOrCreateHostedAIWallet(userID string, tier Tier, now time.Time) (*HostedAIWallet, error) {
	allowance := WeeklyHostedAIAllowance(tier)
	now = now.UTC()
	var wallet HostedAIWallet
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `
			WITH released AS (
				UPDATE hosted_ai_reservations SET status='released',settled_at=NOW()
				WHERE user_id=$1 AND status='reserved' AND created_at<NOW()-INTERVAL '15 minutes'
				RETURNING reserved_microusd
			)
			UPDATE hosted_ai_wallets SET reserved_microusd=GREATEST(0,reserved_microusd-COALESCE((SELECT SUM(reserved_microusd) FROM released),0))
			WHERE user_id=$1`, userID); err != nil {
			return err
		}
		result, err := tx.ExecContext(context.Background(), `
			INSERT INTO hosted_ai_wallets(user_id,weekly_allowance_microusd,weekly_remaining_microusd,reset_at)
			VALUES($1,$2,$2,$3) ON CONFLICT(user_id) DO NOTHING`, userID, allowance, nextWeeklyReset(now))
		if err != nil {
			return err
		}
		inserted, _ := result.RowsAffected()
		var priorAllowance, priorRemaining int64
		var priorReset time.Time
		if err := tx.QueryRowContext(context.Background(), `SELECT weekly_allowance_microusd,weekly_remaining_microusd,reset_at FROM hosted_ai_wallets WHERE user_id=$1 FOR UPDATE`, userID).
			Scan(&priorAllowance, &priorRemaining, &priorReset); err != nil {
			return err
		}
		resetDue := inserted == 0 && !priorReset.After(now)
		remaining := priorRemaining
		resetAt := priorReset
		if resetDue {
			remaining = allowance
			resetAt = nextWeeklyReset(now)
		} else if priorAllowance != allowance {
			used := priorAllowance - priorRemaining
			if used < 0 {
				used = 0
			}
			remaining = allowance - used
			if remaining < 0 {
				remaining = 0
			}
		}
		if _, err := tx.ExecContext(context.Background(), `UPDATE hosted_ai_wallets SET weekly_allowance_microusd=$2,weekly_remaining_microusd=$3,reset_at=$4,updated_at=NOW() WHERE user_id=$1`, userID, allowance, remaining, resetAt); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT user_id,weekly_allowance_microusd,weekly_remaining_microusd,reserved_microusd,reset_at FROM hosted_ai_wallets WHERE user_id=$1`, userID).
			Scan(&wallet.UserID, &wallet.WeeklyAllowanceMicrousd, &wallet.WeeklyRemainingMicrousd, &wallet.ReservedMicrousd, &wallet.ResetAt); err != nil {
			return err
		}
		if inserted > 0 || resetDue {
			return insertHostedAILedgerEntry(tx, userID, "weekly_grant", allowance, "weekly_grant:"+userID+":"+wallet.ResetAt.Format(time.RFC3339))
		}
		if remaining != priorRemaining {
			return insertHostedAILedgerEntry(tx, userID, "plan_adjustment", remaining-priorRemaining, "plan_adjustment:"+userID+":"+now.Format(time.RFC3339Nano))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}

func (db *Database) GetOrCreateCreditWallet(userID string, tier Tier, now time.Time) (*CreditWallet, error) {
	return db.GetOrCreateHostedAIWallet(userID, tier, now)
}

func (db *Database) StartSubscriptionCreditPeriod(userID string, tier Tier, activatedAt time.Time, _ string) (*CreditWallet, error) {
	return db.GetOrCreateHostedAIWallet(userID, tier, activatedAt)
}

func insertHostedAILedgerEntry(tx *sql.Tx, userID, source string, delta int64, idempotencyKey string) error {
	_, err := tx.ExecContext(context.Background(), `INSERT INTO hosted_ai_usage_ledger(id,user_id,source,weekly_delta_microusd,idempotency_key)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT(idempotency_key) DO NOTHING`, uuid.NewString(), userID, source, delta, idempotencyKey)
	return err
}

func (db *Database) ReserveHostedAIUsage(userID string, tier Tier, meter, idempotencyKey string, amount int64, now time.Time) (*HostedAIReservation, *HostedAIWallet, error) {
	if amount <= 0 || strings.TrimSpace(idempotencyKey) == "" {
		return nil, nil, errors.New("invalid hosted AI reservation")
	}
	if _, err := db.GetOrCreateHostedAIWallet(userID, tier, now); err != nil {
		return nil, nil, err
	}
	reservation := &HostedAIReservation{ID: uuid.NewString(), UserID: userID, ReservedMicrousd: amount, ReservedCredits: amount, Status: "reserved"}
	var wallet HostedAIWallet
	reservedNow := false
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(context.Background(), `SELECT user_id,weekly_allowance_microusd,weekly_remaining_microusd,reserved_microusd,reset_at FROM hosted_ai_wallets WHERE user_id=$1 FOR UPDATE`, userID).
			Scan(&wallet.UserID, &wallet.WeeklyAllowanceMicrousd, &wallet.WeeklyRemainingMicrousd, &wallet.ReservedMicrousd, &wallet.ResetAt); err != nil {
			return err
		}
		var existingID, existingUserID, existingMeter, existingStatus string
		var existingAmount int64
		existingErr := tx.QueryRowContext(context.Background(), `SELECT id,user_id,meter,reserved_microusd,status FROM hosted_ai_reservations WHERE idempotency_key=$1 FOR UPDATE`, idempotencyKey).
			Scan(&existingID, &existingUserID, &existingMeter, &existingAmount, &existingStatus)
		if existingErr == nil {
			if existingUserID != userID || existingMeter != meter || existingAmount != amount {
				return errors.New("hosted AI idempotency key reused with different reservation parameters")
			}
			reservation.ID, reservation.Status = existingID, existingStatus
			if existingStatus != "released" {
				return nil
			}
			if wallet.Available() < amount {
				return HostedAILimitReachedError{Available: wallet.Available(), Required: amount}
			}
			if _, err := tx.ExecContext(context.Background(), `UPDATE hosted_ai_reservations SET status='reserved',created_at=NOW(),settled_at=NULL WHERE id=$1`, existingID); err != nil {
				return err
			}
			reservation.Status = "reserved"
			reservedNow = true
			_, err := tx.ExecContext(context.Background(), `UPDATE hosted_ai_wallets SET reserved_microusd=reserved_microusd+$2,updated_at=NOW() WHERE user_id=$1`, userID, amount)
			return err
		}
		if !errors.Is(existingErr, sql.ErrNoRows) {
			return existingErr
		}
		if wallet.Available() < amount {
			return HostedAILimitReachedError{Available: wallet.Available(), Required: amount}
		}
		if _, err := tx.ExecContext(context.Background(), `INSERT INTO hosted_ai_reservations(id,user_id,idempotency_key,meter,reserved_microusd) VALUES($1,$2,$3,$4,$5)`, reservation.ID, userID, idempotencyKey, meter, amount); err != nil {
			return err
		}
		reservedNow = true
		_, err := tx.ExecContext(context.Background(), `UPDATE hosted_ai_wallets SET reserved_microusd=reserved_microusd+$2,updated_at=NOW() WHERE user_id=$1`, userID, amount)
		return err
	})
	if err != nil {
		return nil, &wallet, err
	}
	if reservedNow {
		wallet.ReservedMicrousd += amount
	}
	return reservation, &wallet, nil
}

func (db *Database) ReserveCredits(userID string, tier Tier, meter, idempotencyKey string, amount int64, now time.Time) (*CreditReservation, *CreditWallet, error) {
	return db.ReserveHostedAIUsage(userID, tier, meter, idempotencyKey, amount, now)
}

func (db *Database) ReleaseHostedAIReservation(reservationID string) error {
	return db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		var userID string
		var reserved int64
		err := tx.QueryRowContext(context.Background(), `UPDATE hosted_ai_reservations SET status='released',settled_at=NOW() WHERE id=$1 AND status='reserved' RETURNING user_id,reserved_microusd`, reservationID).Scan(&userID, &reserved)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), `UPDATE hosted_ai_wallets SET reserved_microusd=GREATEST(0,reserved_microusd-$2),updated_at=NOW() WHERE user_id=$1`, userID, reserved)
		return err
	})
}

func (db *Database) ReleaseCreditReservation(reservationID string) error {
	return db.ReleaseHostedAIReservation(reservationID)
}

func (db *Database) SettleHostedAIReservation(reservationID, idempotencyKey string, usage HostedAIUsage) (*HostedAIWallet, error) {
	charge := usage.ChargeMicrousd
	if charge <= 0 {
		charge = usage.Credits
	}
	if charge < 1 {
		charge = 1
	}
	var wallet HostedAIWallet
	err := db.withRLSContext(context.Background(), serviceRLSSettings(), func(tx *sql.Tx) error {
		var userID, meter, status string
		var reserved int64
		if err := tx.QueryRowContext(context.Background(), `SELECT user_id,meter,reserved_microusd,status FROM hosted_ai_reservations WHERE id=$1 FOR UPDATE`, reservationID).Scan(&userID, &meter, &reserved, &status); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT user_id,weekly_allowance_microusd,weekly_remaining_microusd,reserved_microusd,reset_at FROM hosted_ai_wallets WHERE user_id=$1 FOR UPDATE`, userID).
			Scan(&wallet.UserID, &wallet.WeeklyAllowanceMicrousd, &wallet.WeeklyRemainingMicrousd, &wallet.ReservedMicrousd, &wallet.ResetAt); err != nil {
			return err
		}
		if status != "reserved" {
			return nil
		}
		maximumCharge := wallet.WeeklyRemainingMicrousd - (wallet.ReservedMicrousd - reserved)
		if maximumCharge < 0 {
			maximumCharge = 0
		}
		if charge > maximumCharge {
			charge = maximumCharge
		}
		if _, err := tx.ExecContext(context.Background(), `UPDATE hosted_ai_wallets SET weekly_remaining_microusd=weekly_remaining_microusd-$2,reserved_microusd=GREATEST(0,reserved_microusd-$3),updated_at=NOW() WHERE user_id=$1`, userID, charge, reserved); err != nil {
			return err
		}
		if _, err := tx.ExecContext(context.Background(), `UPDATE hosted_ai_reservations SET status='settled',settled_at=NOW() WHERE id=$1`, reservationID); err != nil {
			return err
		}
		rateCardVersion := strings.TrimSpace(os.Getenv("MISTY_HOSTED_AI_RATE_CARD_VERSION"))
		if rateCardVersion == "" {
			rateCardVersion = HostedAIRateCardVersion
		}
		_, err := tx.ExecContext(context.Background(), `INSERT INTO hosted_ai_usage_ledger(id,user_id,reservation_id,source,meter,weekly_delta_microusd,provider,model,input_tokens,cached_input_tokens,output_tokens,reasoning_tokens,rate_card_version,provider_cost_microusd,charged_microusd,idempotency_key)
			VALUES($1,$2,$3,'consumption',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT(idempotency_key) DO NOTHING`, uuid.NewString(), userID, reservationID, meter, -charge, usage.Provider, usage.Model, usage.InputTokens, usage.CachedInputTokens, usage.OutputTokens, usage.ReasoningTokens, rateCardVersion, usage.ProviderCost, charge, idempotencyKey)
		if err == nil {
			wallet.WeeklyRemainingMicrousd -= charge
			wallet.ReservedMicrousd -= reserved
		}
		return err
	})
	return &wallet, err
}

func (db *Database) SettleCreditReservation(reservationID, idempotencyKey string, usage CreditUsage) (*CreditWallet, error) {
	return db.SettleHostedAIReservation(reservationID, idempotencyKey, usage)
}

func (db *Database) HostedAIUsageByMeter(userID string, since time.Time) ([]HostedAIUsageSummary, error) {
	var summaries []HostedAIUsageSummary
	err := db.withRLSContext(context.Background(), userRLSSettings(userID), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(context.Background(), `SELECT meter,COALESCE(SUM(charged_microusd),0) FROM hosted_ai_usage_ledger WHERE user_id=$1 AND source='consumption' AND created_at>=$2 GROUP BY meter ORDER BY meter`, userID, since)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var summary HostedAIUsageSummary
			if err := rows.Scan(&summary.Meter, &summary.ChargedMicrousd); err != nil {
				return err
			}
			summaries = append(summaries, summary)
		}
		return rows.Err()
	})
	return summaries, err
}

func (db *Database) CreditUsageByMeter(userID string, since time.Time) ([]CreditUsageSummary, error) {
	return db.HostedAIUsageByMeter(userID, since)
}

var ErrCreditPurchasesRetired = errors.New("credit purchases are retired")

func (db *Database) AddPurchasedCredits(string, string, string, int64) error {
	return ErrCreditPurchasesRetired
}
func (db *Database) RecordCreditPurchase(CreditPurchase) error { return ErrCreditPurchasesRetired }
func (db *Database) GetCreditPurchaseByPaymentIntent(string) (*CreditPurchase, error) {
	return nil, nil
}
func (db *Database) RefundCreditPurchase(*CreditPurchase) error { return nil }
