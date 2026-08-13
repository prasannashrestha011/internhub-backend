package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Internship struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	// Employer who posted the internship
	IssuedBy uuid.UUID `gorm:"type:uuid;index;not null" json:"issued_by"`

	// Basic Information
	Title       string `gorm:"size:255;not null" json:"title"`
	Description string `gorm:"type:text;not null" json:"description"`
	Location    string `gorm:"size:255" json:"location"`
	WorkMode    string `gorm:"size:50;not null" json:"work_mode"`
	// onsite, remote, hybrid

	// Internship Details
	InternshipType string `gorm:"size:50" json:"internship_type"`
	// paid, unpaid

	Duration     int    `gorm:"default:0" json:"duration"`
	DurationUnit string `gorm:"size:30" json:"duration_unit"`
	// weeks, months

	WorkingHours string `gorm:"size:100" json:"working_hours"`
	// e.g. "10:00 AM - 5:00 PM"

	// Requirements
	RequiredSkills    string `gorm:"type:text" json:"required_skills"`
	PreferredSkills   string `gorm:"type:text" json:"preferred_skills"`
	RequiredEducation string `gorm:"size:255" json:"required_education"`

	// Student eligibility
	EligiblePrograms string `gorm:"type:text" json:"eligible_programs"`
	// e.g. BSc CSIT, BIT, BCA

	EligibleSemester string `gorm:"size:100" json:"eligible_semester"`
	// e.g. 5th semester and above

	// Stipend
	StipendAmount   float64 `gorm:"default:0" json:"stipend_amount"`
	StipendCurrency string  `gorm:"size:10;default:NPR" json:"stipend_currency"`
	StipendPeriod   string  `gorm:"size:30" json:"stipend_period"`
	// monthly, weekly, fixed

	// Vacancy
	VacancyCount int `gorm:"default:1" json:"vacancy_count"`

	// Internship Timeline
	StartDate           *time.Time `json:"start_date,omitempty"`
	ApplicationDeadline *time.Time `json:"application_deadline,omitempty"`

	// Application
	ApplicationEmail string `gorm:"size:255" json:"application_email,omitempty"`
	ApplicationURL   string `gorm:"size:500" json:"application_url,omitempty"`

	// Additional Information
	Responsibilities string `gorm:"type:text" json:"responsibilities"`
	Benefits         string `gorm:"type:text" json:"benefits"`

	// Status
	Status string `gorm:"size:30;default:draft;index" json:"status"`
	// draft, published, closed, expired

	IsActive bool `gorm:"default:true" json:"is_active"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (i *Internship) BeforeCreate(tx *gorm.DB) error {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}

	return nil
}
