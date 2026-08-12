package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StudentDocument represents a document belonging to a student's profile.
// Fields:
//   - ID: UUID primary key
//   - ProfileID: related profile's UUID
//   - UserID: owner user's UUID
//   - ObjectKey: object storage key (e.g., S3 key)
//   - FileName: original file name
//   - MimeType: MIME type of the file
//   - Size: file size in bytes
//   - IsDefault: whether this is the default document for the profile
//   - CreatedAt, UpdatedAt, DeletedAt: timestamps managed by GORM
type StudentDocument struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	ProfileID uuid.UUID      `gorm:"type:uuid;not null;index" json:"profile_id"`
	UserID    uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	ObjectKey string         `gorm:"type:text;not null" json:"object_key"`
	FileName  string         `gorm:"type:text;not null" json:"file_name"`
	MimeType  string         `gorm:"type:text" json:"mime_type"`
	Size      int64          `json:"size"`
	IsDefault bool           `gorm:"default:false" json:"is_default"`
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
