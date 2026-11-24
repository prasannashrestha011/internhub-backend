package repositories

import (
	"crypto/subtle"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInvalidVerificationCode  = errors.New("code is invalid or expired; request a new code if needed")
	ErrVerificationCooldown     = errors.New("wait 60 seconds before requesting another code")
	ErrVerificationSendLimit    = errors.New("too many verification emails; try again in an hour")
	ErrVerificationAttemptLimit = errors.New("too many incorrect codes; try again in an hour")
	ErrAlreadyVerified          = errors.New("email is already verified")
)

func (r *UserRepository) CreatePendingUser(user *models.User) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return tx.Create(&models.EmailVerification{UserID: user.ID}).Error
	})
}

func (r *UserRepository) ReserveVerificationCode(userID uuid.UUID, hash string, now time.Time) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		// Both send and verify lock the user first, serializing account changes across replicas.
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if user.CanSignIn() {
			return ErrAlreadyVerified
		}
		var verification models.EmailVerification
		if err := tx.First(&verification, "user_id = ?", userID).Error; err != nil {
			return err
		}
		//compare with last timestamp to check the limit
		if verification.LastSentAt != nil && now.Sub(*verification.LastSentAt) < time.Minute {
			return ErrVerificationCooldown
		}
		//resets verification after 1 hour
		if now.Sub(verification.SendWindowStart) >= time.Hour {
			verification.SendWindowStart, verification.SendCount = now, 0
		}
		if now.Sub(verification.AttemptWindowStart) >= time.Hour {
			verification.AttemptWindowStart, verification.FailedAttempts = now, 0
		}
		//checks the limit, if it exceeds count 5, restrict
		if verification.SendCount >= 5 {
			return ErrVerificationSendLimit
		}
		if verification.FailedAttempts >= 5 {
			return ErrVerificationAttemptLimit
		}
		//set the verified values
		verification.CodeHash, verification.ExpiresAt, verification.LastSentAt = hash, now.Add(10*time.Minute), &now
		verification.SendCount++
		return tx.Save(&verification).Error
	})
}

func (r *UserRepository) VerifyEmailCode(userID uuid.UUID, hash string, now time.Time) error {
	var result error
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if user.CanSignIn() {
			return ErrInvalidVerificationCode
		}
		var verification models.EmailVerification
		if err := tx.First(&verification, "user_id = ?", userID).Error; err != nil {
			return err
		}
		if verification.CodeHash == "" || !now.Before(verification.ExpiresAt) {
			return ErrInvalidVerificationCode
		}
		//resets the attemptwindowstart and failed attemp once 1 hour is passed after last attempt
		if now.Sub(verification.AttemptWindowStart) >= time.Hour {
			verification.AttemptWindowStart, verification.FailedAttempts = now, 0
		}
		if verification.FailedAttempts >= 5 {
			return ErrVerificationAttemptLimit
		}
		//verify the code hash, if failed increase the failed attempt
		if subtle.ConstantTimeCompare([]byte(verification.CodeHash), []byte(hash)) != 1 {
			verification.FailedAttempts++
			result = ErrInvalidVerificationCode
			if verification.FailedAttempts >= 5 {
				result = ErrVerificationAttemptLimit
			}
			// Return nil after saving so wrong guesses commit instead of rolling back.
			return tx.Save(&verification).Error
		}
		if err := tx.Model(&user).Update("email_verified_at", now).Error; err != nil {
			return err
		}
		return tx.Model(&verification).Update("code_hash", "").Error
	})
	if err != nil {
		return err
	}
	return result
}
