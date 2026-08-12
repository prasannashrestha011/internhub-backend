package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/enums"
)

type OrganizationVerification struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	EmployerProfileID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"employer_profile_id"`

	Status enums.OrganizationVerificationStatus `gorm:"size:30;not null;default:'pending'" json:"status"`

	Method enums.OrganizationVerificationMethod `gorm:":30" json:"method,omitempty"`

	// Domain/email verification
	OrganizationEmail string `gorm:"size:255" json:"organization_email,omitempty"`
	EmailDomain       string `gorm:"size:255" json:"email_domain,omitempty"`

	// Document verification
	DocumentType string `gorm:"size:100" json:"document_type,omitempty"`

	// Store MinIO object key only, NOT presigned URL
	DocumentObjectKey string `gorm:"type:text" json:"-"`

	// Reviewer information
	ReviewedBy *uuid.UUID `gorm:"type:uuid" json:"reviewed_by,omitempty"`

	RejectionReason string `gorm:"type:text" json:"rejection_reason,omitempty"`
	ReviewNotes     string `gorm:"type:text" json:"review_notes,omitempty"`

	SubmittedAt *time.Time `json:"submitted_at,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	VerifiedAt  *time.Time `json:"verified_at,omitempty"`

	EmployerProfile *EmployerProfile `gorm:"foreignKey:EmployerProfileID" json:"employer_profile,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
