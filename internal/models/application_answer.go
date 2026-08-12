package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ApplicationAnswer stores question/answer pairs provided during application
type ApplicationAnswer struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	ApplicationID uuid.UUID `gorm:"type:uuid;index;not null" json:"application_id"`
	Question      string    `gorm:"type:text" json:"question"`
	Answer        string    `gorm:"type:text" json:"answer"`
	CreatedAt     time.Time `json:"created_at"`
}

func (a *ApplicationAnswer) BeforeCreate(tx *gorm.DB) (err error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
