package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Job struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	// Foreign Keys
	IssuedBy uuid.UUID `gorm:"type:uuid;index;not null" json:"issued_by"`

	// Basic Job Information
	Title       string `gorm:"size:255;not null" json:"title"`
	Description string `gorm:"type:text" json:"description"`
	Location    string `gorm:"size:255" json:"location"`
	Remote      bool   `json:"remote"`

	// Job Classification
	JobType         string `gorm:"size:50" json:"job_type"`         // full_time, part_time, internship, contract
	WorkMode        string `gorm:"size:50" json:"work_mode"`        // onsite, remote, hybrid
	ExperienceLevel string `gorm:"size:50" json:"experience_level"` // entry, junior, mid

	// Requirements
	RequiredSkills     string `gorm:"type:text" json:"required_skills"`
	RequiredEducation  string `gorm:"size:255" json:"required_education"`
	ExperienceRequired int    `gorm:"default:0" json:"experience_required"`

	// Salary
	SalaryMin      float64 `json:"salary_min"`
	SalaryMax      float64 `json:"salary_max"`
	SalaryCurrency string  `gorm:"size:10;default:NPR" json:"salary_currency"`
	SalaryPeriod   string  `gorm:"size:30" json:"salary_period"` // hourly, monthly, yearly

	// Vacancy Information
	VacancyCount int `gorm:"default:1" json:"vacancy_count"`

	// Application
	ApplicationDeadline *time.Time `json:"application_deadline"`
	ApplicationEmail    string     `gorm:"size:255" json:"application_email"`
	ApplicationURL      string     `gorm:"size:500" json:"application_url"`

	// Status
	IsActive bool   `gorm:"default:true" json:"is_active"`
	Status   string `gorm:"size:30;default:draft" json:"status"` // draft, published, closed, expired

	// Additional
	Benefits string `gorm:"type:text" json:"benefits"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (j *Job) BeforeCreate(tx *gorm.DB) (err error) {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	return nil
}
