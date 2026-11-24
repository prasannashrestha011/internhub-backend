package database

import (
	"fmt"
	"time"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"gorm.io/gorm"
)

func AutoMigrate(db *gorm.DB) error {
	// Keep the column and legacy backfill atomic so a restart cannot verify new accounts.
	return db.Transaction(func(tx *gorm.DB) error {
		hadVerification := tx.Migrator().HasColumn(&models.User{}, "EmailVerifiedAt")
		if err := tx.AutoMigrate(&models.User{}); err != nil {
			return err
		}
		if !hadVerification {
			if err := tx.Unscoped().Model(&models.User{}).Where("email_verified_at IS NULL").Update("email_verified_at", time.Now().UTC()).Error; err != nil {
				return err
			}
		}
		// Retired tables and columns are retained so existing records are not destroyed.
		if err := tx.AutoMigrate(&models.EmailVerification{}, &models.RefreshToken{}, &models.StudentProfile{}, &models.StudentDocument{}, &models.RecruiterProfile{}, &models.Internship{}, &models.InternshipApplication{}); err != nil {
			return err
		}
		if err := tx.Model(&models.InternshipApplication{}).Where("status IN ?", []string{"reviewing", "shortlisted"}).Update("status", models.ApplicationStatusSubmitted).Error; err != nil {
			return err
		}
		// SQLite table recreation can drop expression indexes, so recreate this last.
		if err := tx.Exec("CREATE UNIQUE INDEX IF NOT EXISTS users_email_normalized ON users (LOWER(TRIM(email)))").Error; err != nil {
			return fmt.Errorf("email uniqueness migration: resolve duplicate emails ignoring case and whitespace: %w", err)
		}
		return nil
	})
}
