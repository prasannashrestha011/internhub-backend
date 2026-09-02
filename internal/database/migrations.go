package database

import (
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"gorm.io/gorm"
)

// AutoMigrate keeps the API and standalone database commands on the same schema.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.RefreshToken{},
		&models.StudentProfile{},
		&models.StudentEducation{},
		&models.StudentSkill{},
		&models.StudentProject{},
		&models.StudentCertification{},
		&models.StudentDocument{},
		&models.Internship{},
		&models.InternshipApplication{},
		&models.Interview{},
		&models.RecruiterProfile{},
		&models.OrganizationVerification{},
	)
}
