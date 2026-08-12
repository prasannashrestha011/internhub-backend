package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Interview represents a scheduled interview between employer and student for an application or job
type Interview struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	JobID         uuid.UUID `gorm:"type:uuid;index;not null" json:"job_id"`
	ApplicationID uuid.UUID `gorm:"type:uuid;index" json:"application_id"`
	EmployerID    uuid.UUID `gorm:"type:uuid;index;not null" json:"employer_id"`
	StudentID     uuid.UUID `gorm:"type:uuid;index;not null" json:"student_id"`
	ScheduledAt   time.Time `json:"scheduled_at"`
	DurationMins  int       `json:"duration_mins"`
	Location      string    `gorm:"size:255" json:"location"`
	Remote        bool      `json:"remote"`
	Status        string    `gorm:"size:64;default:'scheduled'" json:"status"` // scheduled, accepted, declined, cancelled, completed
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (i *Interview) BeforeCreate(tx *gorm.DB) (err error) {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}
	return nil
}
