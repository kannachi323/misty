package db

import (
	"database/sql"
	"errors"
	"log"
	"time"
)

type Tier string

const (
	TierBasic    Tier = "basic"
	TierPersonal Tier = "personal"
	TierPro      Tier = "pro"
)

const (
	LicenseStatusActive   = "active"
	LicenseStatusTrialing = "trialing"
)

type License struct {
	ID             string
	UserID         string
	Tier           Tier
	Status         string
	ExpiresAt      *time.Time
	TrialStartedAt *time.Time
	LicenseDevice  string
}

func createLicenseTx(tx *sql.Tx, licenseID string, userID string, tier Tier, status string, expiresAt *time.Time) (*License, error) {
	license := &License{
		ID:             licenseID,
		UserID:         userID,
		Tier:           tier,
		Status:         status,
		ExpiresAt:      expiresAt,
		TrialStartedAt: nil,
	}

	_, err := tx.Exec(
		`INSERT INTO licenses (id, user_id, tier, status, expires_at, trial_started_at, license_device) VALUES ($1, $2, $3, $4, $5, $6, '')`,
		license.ID, license.UserID, license.Tier, license.Status, license.ExpiresAt, license.TrialStartedAt,
	)
	if err != nil {
		return nil, err
	}

	return license, nil
}

func (db *Database) GetLicenseByUserID(userID string) (*License, error) {
	var lic License
	var expiresAt sql.NullTime
	var trialStartedAt sql.NullTime

	err := db.Conn.QueryRow(
		`SELECT id, user_id, tier, status, expires_at, trial_started_at, license_device FROM licenses WHERE user_id = $1`,
		userID,
	).Scan(&lic.ID, &lic.UserID, &lic.Tier, &lic.Status, &expiresAt, &trialStartedAt, &lic.LicenseDevice)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		log.Println("Failed to get license:", err)
		return nil, err
	}

	if expiresAt.Valid {
		lic.ExpiresAt = &expiresAt.Time
	}
	if trialStartedAt.Valid {
		lic.TrialStartedAt = &trialStartedAt.Time
	}

	if lic.Status == LicenseStatusTrialing && lic.ExpiresAt != nil && !lic.ExpiresAt.After(time.Now()) {
		if err := db.SetLicenseStateByID(lic.ID, TierBasic, LicenseStatusActive, nil); err != nil {
			return nil, err
		}
		lic.Tier = TierBasic
		lic.Status = LicenseStatusActive
		lic.ExpiresAt = nil
	}

	return &lic, nil
}

func (db *Database) StartTrialByUserID(userID string, duration time.Duration) (bool, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(duration)

	result, err := db.Conn.Exec(`
		UPDATE licenses
		SET tier = $2,
			status = $3,
			expires_at = $4,
			trial_started_at = $5,
			updated_at = NOW()
		WHERE user_id = $1
			AND tier = $6
			AND status = $7
			AND trial_started_at IS NULL
	`, userID, TierPersonal, LicenseStatusTrialing, expiresAt, now, TierBasic, LicenseStatusActive)
	if err != nil {
		log.Println("Failed to start trial:", err)
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected == 1, nil
}

func (db *Database) SetLicenseStateByID(licenseID string, tier Tier, status string, expiresAt *time.Time) error {
	_, err := db.Conn.Exec(`
		UPDATE licenses
		SET tier = $2,
			status = $3,
			expires_at = $4,
			updated_at = NOW()
		WHERE id = $1
	`, licenseID, tier, status, expiresAt)
	if err != nil {
		log.Println("Failed to update license:", err)
	}
	return err
}

func (db *Database) SetLicenseStateByUserID(userID string, tier Tier, status string, expiresAt *time.Time) error {
	_, err := db.Conn.Exec(`
		UPDATE licenses
		SET tier = $2,
			status = $3,
			expires_at = $4,
			updated_at = NOW()
		WHERE user_id = $1
	`, userID, tier, status, expiresAt)
	if err != nil {
		log.Println("Failed to update license:", err)
	}
	return err
}

func (db *Database) UpdateLicenseDevice(userID, device string) error {
	_, err := db.Conn.Exec(`
		UPDATE licenses
		SET license_device = $2,
			updated_at = NOW()
		WHERE user_id = $1
	`, userID, device)
	if err != nil {
		log.Println("Failed to update license device:", err)
	}
	return err
}
