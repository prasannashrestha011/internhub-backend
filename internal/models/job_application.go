package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// JobApplication is a placeholder model for Phase 4 — stores basic application metadata.
type JobApplication struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	JobID     uuid.UUID `gorm:"type:uuid;index;not null" json:"job_id"`
	StudentID uuid.UUID `gorm:"type:uuid;index;not null" json:"student_id"`
	CoverNote string    `gorm:"type:text" json:"cover_note"`
	ResumeKey string    `gorm:"size:512" json:"resume_key"`
	Status    string    `gorm:"size:64;default:'applied'" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (a *JobApplication) BeforeCreate(tx *gorm.DB) (err error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
