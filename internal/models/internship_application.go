package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type InternshipApplicationStatus string

const (
	ApplicationStatusSubmitted   InternshipApplicationStatus = "submitted"
	ApplicationStatusReviewing   InternshipApplicationStatus = "reviewing"
	ApplicationStatusShortlisted InternshipApplicationStatus = "shortlisted"
	ApplicationStatusRejected    InternshipApplicationStatus = "rejected"
	ApplicationStatusAccepted    InternshipApplicationStatus = "accepted"
	ApplicationStatusWithdrawn   InternshipApplicationStatus = "withdrawn"
)

type InternshipApplication struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	InternshipID uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_student_internship" json:"internship_id"`
	StudentID    uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_student_internship" json:"student_id"`

	Status InternshipApplicationStatus `gorm:"size:30;not null;default:submitted;index" json:"status"`

	EmployerNote string `gorm:"type:text" json:"employer_note,omitempty"`

	AppliedAt     time.Time  `gorm:"not null" json:"applied_at"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	ShortlistedAt *time.Time `json:"shortlisted_at,omitempty"`
	AcceptedAt    *time.Time `json:"accepted_at,omitempty"`
	RejectedAt    *time.Time `json:"rejected_at,omitempty"`
	WithdrawnAt   *time.Time `json:"withdrawn_at,omitempty"`

	Internship *Internship     `gorm:"foreignKey:InternshipID;references:ID" json:"internship,omitempty"`
	Student    *StudentProfile `gorm:"foreignKey:StudentID;references:ID" json:"student,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func (a *InternshipApplication) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}

	if a.AppliedAt.IsZero() {
		a.AppliedAt = time.Now()
	}

	if a.Status == "" {
		a.Status = ApplicationStatusSubmitted
	}

	return nil
}
