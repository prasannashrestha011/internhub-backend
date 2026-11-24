package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StudentDocument stores the student CV.
type StudentDocument struct {
	URL       string         `gorm:"-" json:"url,omitempty"`
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	ProfileID uuid.UUID      `gorm:"type:uuid;not null;index" json:"profile_id"`
	UserID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	ObjectKey string         `gorm:"type:text;not null" json:"-"`
	FileName  string         `gorm:"type:text;not null" json:"file_name"`
	MimeType  string         `gorm:"type:text" json:"mime_type"`
	Size      int64          `json:"size"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at"`
}

// BeforeCreate hook sets a UUID primary key if not already set.
func (sd *StudentDocument) BeforeCreate(tx *gorm.DB) (err error) {
	if sd.ID == uuid.Nil {
		sd.ID = uuid.New()
	}
	return nil
}
