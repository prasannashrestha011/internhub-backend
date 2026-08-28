package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// StudentProfile holds extended profile information for student users
type StudentProfile struct {
	ID                       uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	UserID                   uuid.UUID      `gorm:"type:uuid;uniqueIndex;not null"`
	FullName                 string         `gorm:"size:255" json:"full_name"`
	Phone                    string         `gorm:"size:50" json:"phone"`
	Location                 string         `gorm:"size:255" json:"location"`
	Bio                      string         `gorm:"type:text" json:"bio"`
	CollegeName              string         `gorm:"size:255" json:"college_name"`
	Degree                   string         `gorm:"size:255" json:"degree"`
	FacultyOrMajor           string         `gorm:"size:255" json:"faculty_or_major"`
	CurrentSemester          string         `gorm:"size:64" json:"current_semester"`
	GraduationYear           int            `json:"graduation_year"`
	PreferredJobCategories   string         `gorm:"type:text" json:"preferred_job_categories"`
	PreferredLocations       string         `gorm:"type:text" json:"preferred_locations"`
	PreferredWorkMode        string         `gorm:"size:64" json:"preferred_work_mode"`
	Availability             string         `gorm:"size:64" json:"availability"`
	ExpectedSalary           string         `gorm:"size:64" json:"expected_salary"`
	LinkedinURL              string         `gorm:"size:255" json:"linkedin_url"`
	GithubURL                string         `gorm:"size:255" json:"github_url"`
	PortfolioURL             string         `gorm:"size:255" json:"portfolio_url"`
	ProfileImageKey          string         `gorm:"size:512" json:"profile_image_key"`
	IsSearchable             bool           `gorm:"default:true" json:"is_searchable"`
	ProfileCompletionPercent int            `gorm:"default:0" json:"profile_completion_percentage"`
	CreatedAt                time.Time      `json:"created_at"`
	UpdatedAt                time.Time      `json:"updated_at"`
	DeletedAt                gorm.DeletedAt `gorm:"index" json:"-"`

	Documents []StudentDocument `gorm:"foreignKey:ProfileID;references:ID" json:"documents,omitempty"`
}

func (s *StudentProfile) BeforeCreate(tx *gorm.DB) (err error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
