package repositories

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

func TestStudentRepositoryGetApplicationStats(t *testing.T) {
	dsn := fmt.Sprintf("file:student-stats-%s?mode=memory&cache=shared", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE internship_applications (
			id TEXT PRIMARY KEY,
			student_id TEXT NOT NULL,
			status TEXT NOT NULL
		)
	`).Error; err != nil {
		t.Fatalf("create applications table: %v", err)
	}

	studentID := uuid.New()
	statuses := []models.InternshipApplicationStatus{
		models.ApplicationStatusSubmitted,
		models.ApplicationStatusSubmitted,
		models.ApplicationStatusAccepted,
		models.ApplicationStatusAccepted,
		models.ApplicationStatusRejected,
		models.ApplicationStatusWithdrawn,
	}
	for _, status := range statuses {
		if err := db.Exec(
			"INSERT INTO internship_applications (id, student_id, status) VALUES (?, ?, ?)",
			uuid.New(), studentID, status,
		).Error; err != nil {
			t.Fatalf("insert %s application: %v", status, err)
		}
	}

	repo := NewStudentRepository(db)
	stats, err := repo.GetApplicationStats(context.Background(), studentID)
	if err != nil {
		t.Fatalf("get application stats: %v", err)
	}

	want := StudentApplicationStats{
		TotalApplications:     6,
		ApprovedApplications:  2,
		RejectedApplications:  1,
		PendingApplications:   2,
		WithdrawnApplications: 1,
	}
	if *stats != want {
		t.Fatalf("stats = %+v, want %+v", *stats, want)
	}

	emptyStats, err := repo.GetApplicationStats(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("get empty application stats: %v", err)
	}
	if *emptyStats != (StudentApplicationStats{}) {
		t.Fatalf("empty stats = %+v, want zero values", *emptyStats)
	}
}

func TestSaveResumeReplacesCurrentCV(t *testing.T) {
	db := openApplicationRepositoryTestDB(t)
	student := createRepositoryTestStudent(t, db, "Student")
	repo := NewStudentRepository(db)
	ctx := context.Background()
	first := &models.StudentDocument{ProfileID: student.ID, UserID: student.UserID, FileName: "first.pdf", ObjectKey: "first.pdf"}
	if _, err := repo.SaveResume(ctx, first); err != nil {
		t.Fatal(err)
	}
	replacement := &models.StudentDocument{ProfileID: student.ID, UserID: student.UserID, FileName: "replacement.pdf", ObjectKey: "replacement.pdf"}
	oldKey, err := repo.SaveResume(ctx, replacement)
	if err != nil || oldKey != "first.pdf" || replacement.ID != first.ID {
		t.Fatalf("replacement: old=%s id=%s error=%v", oldKey, replacement.ID, err)
	}
	var count int64
	if err := db.Model(&models.StudentDocument{}).Where("profile_id = ?", student.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("CV count=%d: %v", count, err)
	}
	if _, err := repo.SaveResume(ctx, &models.StudentDocument{ProfileID: student.ID, UserID: uuid.New(), ObjectKey: "other.pdf"}); err == nil {
		t.Fatal("another user replaced the CV")
	}
	current, err := repo.GetResume(ctx, student.ID)
	if err != nil || current.ObjectKey != "replacement.pdf" {
		t.Fatalf("CV=%+v: %v", current, err)
	}
}

func TestDeleteResumeRemovesLegacyCVsAndChecksOwnership(t *testing.T) {
	db := openApplicationRepositoryTestDB(t)
	student := createRepositoryTestStudent(t, db, "Student")
	other := createRepositoryTestStudent(t, db, "Other student")
	repo := NewStudentRepository(db)
	ctx := context.Background()
	for _, document := range []models.StudentDocument{
		{ProfileID: student.ID, UserID: student.UserID, FileName: "old.pdf", ObjectKey: "old.pdf"},
		{ProfileID: student.ID, UserID: student.UserID, FileName: "current.pdf", ObjectKey: "current.pdf"},
		{ProfileID: other.ID, UserID: other.UserID, FileName: "other.pdf", ObjectKey: "other.pdf"},
	} {
		if err := db.Create(&document).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.DeleteResume(ctx, student.ID, other.UserID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("another user deleted the CV: %v", err)
	}
	documents, err := repo.DeleteResume(ctx, student.ID, student.UserID)
	if err != nil || len(documents) != 2 {
		t.Fatalf("deleted documents=%v: %v", documents, err)
	}
	if _, err := repo.GetResume(ctx, student.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted CV fell back to a legacy file: %v", err)
	}
	if document, err := repo.GetResume(ctx, other.ID); err != nil || document.ObjectKey != "other.pdf" {
		t.Fatalf("other student's CV changed: %v, %v", document, err)
	}
	if documents, err := repo.DeleteResume(ctx, student.ID, student.UserID); err != nil || len(documents) != 0 {
		t.Fatalf("repeat deletion: %v, %v", documents, err)
	}
	if _, err := repo.SaveResume(ctx, &models.StudentDocument{ProfileID: student.ID, UserID: student.UserID, FileName: "new.pdf", ObjectKey: "new.pdf"}); err != nil {
		t.Fatalf("upload after deletion: %v", err)
	}
	if document, err := repo.GetResume(ctx, student.ID); err != nil || document.ObjectKey != "new.pdf" {
		t.Fatalf("new CV unavailable: %v, %v", document, err)
	}
}
