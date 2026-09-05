package seeder_test

import (
	"testing"

	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/seeder"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunCreatesAnIdempotentDevelopmentDataset(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:seeder-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	for run := 1; run <= 2; run++ {
		summary, err := seeder.Run(db, seeder.DefaultPassword)
		if err != nil {
			t.Fatalf("seed run %d: %v", run, err)
		}
		if summary.Users != 6 || summary.StudentProfiles != 2 || summary.RecruiterProfiles != 3 ||
			summary.OrganizationVerifications != 3 || summary.Internships != 100 || summary.Applications != 6 {
			t.Fatalf("unexpected summary after run %d: %+v", run, summary)
		}
		if run == 1 {
			var alternate models.Internship
			if err := db.Where("title = ?", "Software Engineering Intern 01").First(&alternate).Error; err != nil {
				t.Fatalf("load alternate internship: %v", err)
			}
			moved := db.Model(&models.InternshipApplication{}).
				Where("status = ?", models.ApplicationStatusSubmitted).
				Update("internship_id", alternate.ID)
			if moved.Error != nil || moved.RowsAffected != 1 {
				t.Fatalf("move seeded application: rows=%d error=%v", moved.RowsAffected, moved.Error)
			}
		}
	}

	assertCount(t, db, &models.User{}, 6)
	assertCount(t, db, &models.StudentProfile{}, 2)
	assertCount(t, db, &models.RecruiterProfile{}, 3)
	assertCount(t, db, &models.OrganizationVerification{}, 3)
	assertCount(t, db, &models.Internship{}, 100)
	assertCount(t, db, &models.InternshipApplication{}, 6)

	var student models.User
	if err := db.Where("email = ?", seeder.StudentEmail).First(&student).Error; err != nil {
		t.Fatalf("load seeded student: %v", err)
	}
	if student.Role != models.RoleStudent {
		t.Fatalf("student role = %q, want %q", student.Role, models.RoleStudent)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(student.PasswordHash), []byte(seeder.DefaultPassword)); err != nil {
		t.Fatalf("seeded password does not match: %v", err)
	}

	var completeProfiles int64
	if err := db.Model(&models.StudentProfile{}).Where("profile_completion_percent = ?", 100).Count(&completeProfiles).Error; err != nil {
		t.Fatalf("count complete profiles: %v", err)
	}
	if completeProfiles != 2 {
		t.Fatalf("complete student profiles = %d, want 2", completeProfiles)
	}

	var approvedRecruiter models.RecruiterProfile
	if err := db.Where("verification_status = ?", enums.OrganizationVerificationApproved).First(&approvedRecruiter).Error; err != nil {
		t.Fatalf("load approved recruiter: %v", err)
	}
	var published int64
	if err := db.Model(&models.Internship{}).
		Where("issued_by = ? AND status = ? AND is_active = ?", approvedRecruiter.UserID, enums.InternshipStatusPublished, true).
		Count(&published).Error; err != nil {
		t.Fatalf("count published internships: %v", err)
	}
	if published != 97 {
		t.Fatalf("published internships = %d, want 97", published)
	}
}

func assertCount(t *testing.T, db *gorm.DB, model interface{}, want int64) {
	t.Helper()
	var got int64
	if err := db.Model(model).Count(&got).Error; err != nil {
		t.Fatalf("count %T: %v", model, err)
	}
	if got != want {
		t.Fatalf("count %T = %d, want %d", model, got, want)
	}
}
