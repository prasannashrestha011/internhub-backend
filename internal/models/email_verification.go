package models

import (
	"time"

	"github.com/google/uuid"
)

// EmailVerification keeps account limits across replacement codes and server restarts.
type EmailVerification struct {
	UserID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	User               User      `gorm:"constraint:OnDelete:CASCADE"`
	CodeHash           string    `gorm:"not null" json:"-"`
	ExpiresAt          time.Time
	LastSentAt         *time.Time
	SendWindowStart    time.Time
	SendCount          int
	AttemptWindowStart time.Time
	FailedAttempts     int
}
