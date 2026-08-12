package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StudentSkill struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	ProfileID uuid.UUID      `gorm:"type:uuid;index;not null" json:"profile_id"`
	Name      string         `gorm:"size:255;index" json:"name"`
	Level     string         `gorm:"size:64" json:"level"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *StudentSkill) BeforeCreate(tx *gorm.DB) (err error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
