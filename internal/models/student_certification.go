package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StudentCertification struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	ProfileID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"profile_id"`
	Name       string         `gorm:"size:255" json:"name"`
	Authority  string         `gorm:"size:255" json:"authority"`
	License    string         `gorm:"size:255" json:"license"`
	URL        string         `gorm:"size:512" json:"url"`
	IssuedDate *time.Time     `json:"issued_date"`
	ExpiryDate *time.Time     `json:"expiry_date"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *StudentCertification) BeforeCreate(tx *gorm.DB) (err error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
