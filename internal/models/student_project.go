package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StudentProject represents a project on a student's profile.
type StudentProject struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	ProfileID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"profile_id"`
	Title       string         `gorm:"size:255;not null" json:"title"`
	Description string         `gorm:"type:text" json:"description"`
	Link        string         `gorm:"size:1024" json:"link"`
	StartDate   *time.Time     `json:"start_date"`
	EndDate     *time.Time     `json:"end_date"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at"`
}

// BeforeCreate is a GORM hook that sets the UUID primary key before inserting.
func (sp *StudentProject) BeforeCreate(tx *gorm.DB) (err error) {
	if sp.ID == uuid.Nil {
		sp.ID = uuid.New()
	}
	return nil
}
