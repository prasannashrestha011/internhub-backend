package seeder_test

import (
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/seeder"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestRunCreatesAnIdempotentDevelopmentDataset(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:seeder-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		summary, err := seeder.Run(db, seeder.DefaultPassword)
		if err != nil {
			t.Fatal(err)
		}
		if summary != (seeder.Summary{Users: 5, StudentProfiles: 2, RecruiterProfiles: 3, Internships: 6, Applications: 6}) {
			t.Fatalf("unexpected summary: %+v", summary)
		}
	}
	for _, item := range []struct {
		model interface{}
		count int64
	}{{&models.User{}, 5}, {&models.StudentProfile{}, 2}, {&models.RecruiterProfile{}, 3}, {&models.Internship{}, 6}, {&models.InternshipApplication{}, 6}} {
		var count int64
		if err := db.Model(item.model).Count(&count).Error; err != nil || count != item.count {
			t.Fatalf("count %T = %d: %v", item.model, count, err)
		}
	}
	var student models.User
	if err := db.Where("email = ?", seeder.StudentEmail).First(&student).Error; err != nil {
		t.Fatal(err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(student.PasswordHash), []byte(seeder.DefaultPassword)); err != nil {
		t.Fatal(err)
	}
	var pending int64
	if err := db.Model(&models.InternshipApplication{}).Where("status = ?", models.ApplicationStatusSubmitted).Count(&pending).Error; err != nil || pending != 3 {
		t.Fatalf("submitted applications = %d: %v", pending, err)
	}
}
