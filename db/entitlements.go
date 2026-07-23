package db

import (
	"context"
	"database/sql"
	"time"
)

const (
	FreeStorageBytes = int64(2_000_000_000)
	ProStorageBytes  = int64(50_000_000_000)

	FreeWeeklyHostedAIAllowance = int64(150_000)
	ProWeeklyHostedAIAllowance  = int64(1_000_000)
)

type PlanEntitlements struct {
	Plan                      Tier  `json:"plan"`
	StorageLimitBytes         int64 `json:"storage_limit_bytes"`
	WeeklyHostedAIAllowance   int64 `json:"-"`
	UnlimitedSpaces           bool  `json:"unlimited_spaces"`
	UnlimitedCollaborators    bool  `json:"unlimited_collaborators"`
	UnlimitedAgentDefinitions bool  `json:"unlimited_agent_definitions"`
}

func NormalizePlan(tier Tier) Tier {
	if tier == TierPro || tier == TierMax {
		return TierPro
	}
	return TierBasic
}

func EntitlementsForTier(tier Tier) PlanEntitlements {
	plan := NormalizePlan(tier)
	entitlements := PlanEntitlements{
		Plan: plan, UnlimitedSpaces: true, UnlimitedCollaborators: true,
		UnlimitedAgentDefinitions: true,
	}
	if plan == TierPro {
		entitlements.StorageLimitBytes = ProStorageBytes
		entitlements.WeeklyHostedAIAllowance = ProWeeklyHostedAIAllowance
		return entitlements
	}
	entitlements.StorageLimitBytes = FreeStorageBytes
	entitlements.WeeklyHostedAIAllowance = FreeWeeklyHostedAIAllowance
	return entitlements
}

func entitlementsForUserTx(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (PlanEntitlements, error) {
	var tier Tier
	var status string
	var expiresAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT tier,status,expires_at FROM licenses WHERE user_id=$1`, userID).Scan(&tier, &status, &expiresAt); err != nil {
		return PlanEntitlements{}, err
	}
	if status == LicenseStatusTrialing && expiresAt.Valid && !expiresAt.Time.After(now.UTC()) {
		tier = TierBasic
	}
	return EntitlementsForTier(tier), nil
}

func (db *Database) EntitlementsForUser(ctx context.Context, userID string) (PlanEntitlements, error) {
	var entitlements PlanEntitlements
	err := db.withRLSContext(ctx, userRLSSettings(userID), func(tx *sql.Tx) error {
		var err error
		entitlements, err = entitlementsForUserTx(ctx, tx, userID, time.Now())
		return err
	})
	return entitlements, err
}
