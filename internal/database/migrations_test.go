package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmailVerificationMigrationGrandfathersOnlyExistingUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&models.User{}, "email_verified_at"); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := db.Exec("INSERT INTO users (id, email, password_hash, role, created_at) VALUES (?, ?, ?, ?, ?)", id, "legacy@example.com", "hash", "student", time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	var legacy models.User
	if err := db.First(&legacy, "id = ?", id).Error; err != nil || legacy.EmailVerifiedAt == nil {
		t.Fatalf("legacy account locked out: %v", err)
	}
	pending := &models.User{Email: "pending@example.com", PasswordHash: "hash", Role: models.RoleStudent}
	if err := db.Create(pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(pending, "id = ?", pending.ID).Error; err != nil || pending.EmailVerifiedAt != nil {
		t.Fatalf("restart verified pending account: %v", err)
	}
	duplicate := &models.User{Email: "PENDING@example.com", PasswordHash: "hash", Role: models.RoleEmployer}
	if err := db.Create(duplicate).Error; err == nil {
		t.Fatal("canonical duplicate accepted")
	}
}
