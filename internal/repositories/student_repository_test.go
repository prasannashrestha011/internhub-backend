package repositories

import (
	"context"
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
		models.ApplicationStatusReviewing,
		models.ApplicationStatusShortlisted,
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
		TotalApplications:       8,
		ActiveApplications:      4,
		ApprovedApplications:    2,
		RejectedApplications:    1,
		PendingApplications:     2,
		UnderReviewApplications: 1,
		ShortlistedApplications: 1,
		WithdrawnApplications:   1,
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
