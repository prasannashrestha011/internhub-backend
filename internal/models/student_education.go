package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StudentEducation struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	ProfileID   uuid.UUID      `gorm:"type:uuid;index;not null" json:"profile_id"`
	Institute   string         `gorm:"size:255" json:"institute"`
	Degree      string         `gorm:"size:255" json:"degree"`
	Field       string         `gorm:"size:255" json:"field_of_study"`
	StartYear   int            `json:"start_year"`
	EndYear     int            `json:"end_year"`
	Description string         `gorm:"type:text" json:"description"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *StudentEducation) BeforeCreate(tx *gorm.DB) (err error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
