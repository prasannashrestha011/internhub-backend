package repositories

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
)

func TestInternshipApplicationRepositoryListForEmployer(t *testing.T) {
	db := openApplicationRepositoryTestDB(t)
	employerID := uuid.New()
	otherEmployerID := uuid.New()

	firstInternship := createRepositoryTestInternship(t, db, employerID, "Backend Intern")
	secondInternship := createRepositoryTestInternship(t, db, employerID, "Design Intern")
	otherInternship := createRepositoryTestInternship(t, db, otherEmployerID, "Private Role")
	alice := createRepositoryTestStudent(t, db, "Alice Shrestha")
	bob := createRepositoryTestStudent(t, db, "Bob Rai")

	baseTime := time.Now().Add(-time.Hour)
	createRepositoryTestApplication(t, db, firstInternship.ID, alice.ID, models.ApplicationStatusSubmitted, baseTime)
	newest := createRepositoryTestApplication(t, db, secondInternship.ID, bob.ID, models.ApplicationStatusRejected, baseTime.Add(30*time.Minute))
	createRepositoryTestApplication(t, db, otherInternship.ID, alice.ID, models.ApplicationStatusReviewing, baseTime.Add(45*time.Minute))

	repo := NewInternshipApplicationRepository(db)
	ctx := context.Background()

	items, total, err := repo.ListForEmployer(ctx, RecruiterApplicationFilter{
		EmployerID: employerID,
		Page:       1,
		PageSize:   10,
	})
	if err != nil {
		t.Fatalf("list employer applications: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("got total=%d len=%d, want 2 employer-owned applications", total, len(items))
	}
	if items[0].ID != newest.ID {
		t.Fatalf("first application = %s, want newest %s", items[0].ID, newest.ID)
	}
	if items[0].Student == nil || items[0].Student.FullName != "Bob Rai" {
		t.Fatalf("student preload = %#v, want Bob Rai", items[0].Student)
	}
	if items[0].Internship == nil || items[0].Internship.Title != "Design Intern" {
		t.Fatalf("internship preload = %#v, want Design Intern", items[0].Internship)
	}

	t.Run("candidate search", func(t *testing.T) {
		filtered, filteredTotal, err := repo.ListForEmployer(ctx, RecruiterApplicationFilter{
			EmployerID: employerID,
			Query:      "  ALIce ",
			Page:       1,
			PageSize:   10,
		})
		if err != nil {
			t.Fatalf("search applications: %v", err)
		}
		if filteredTotal != 1 || len(filtered) != 1 || filtered[0].Student.FullName != "Alice Shrestha" {
			t.Fatalf("search result = %#v total=%d, want Alice only", filtered, filteredTotal)
		}
	})

	t.Run("internship and status filters", func(t *testing.T) {
		filtered, filteredTotal, err := repo.ListForEmployer(ctx, RecruiterApplicationFilter{
			EmployerID:   employerID,
			InternshipID: &secondInternship.ID,
			Status:       models.ApplicationStatusRejected,
			Page:         1,
			PageSize:     10,
		})
		if err != nil {
			t.Fatalf("filter applications: %v", err)
		}
		if filteredTotal != 1 || len(filtered) != 1 || filtered[0].ID != newest.ID {
			t.Fatalf("filtered result = %#v total=%d, want newest application", filtered, filteredTotal)
		}
	})

	t.Run("ownership isolation", func(t *testing.T) {
		filtered, filteredTotal, err := repo.ListForEmployer(ctx, RecruiterApplicationFilter{
			EmployerID:   employerID,
			InternshipID: &otherInternship.ID,
			Page:         1,
			PageSize:     10,
		})
		if err != nil {
			t.Fatalf("list other employer internship: %v", err)
		}
		if filteredTotal != 0 || len(filtered) != 0 {
			t.Fatalf("other employer applications leaked: %#v total=%d", filtered, filteredTotal)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		page, pageTotal, err := repo.ListForEmployer(ctx, RecruiterApplicationFilter{
			EmployerID: employerID,
			Page:       1,
			PageSize:   1,
		})
		if err != nil {
			t.Fatalf("paginate applications: %v", err)
		}
		if pageTotal != 2 || len(page) != 1 || page[0].ID != newest.ID {
			t.Fatalf("page = %#v total=%d, want newest of two", page, pageTotal)
		}
	})
}

func TestInternshipApplicationRepositoryListByStudent(t *testing.T) {
	db := openApplicationRepositoryTestDB(t)
	employerID := uuid.New()
	student := createRepositoryTestStudent(t, db, "Samir Adhikari")
	otherStudent := createRepositoryTestStudent(t, db, "Maya Karki")
	baseTime := time.Now().Add(-2 * time.Hour)

	submittedInternship := createRepositoryTestInternship(t, db, employerID, "Backend Intern")
	reviewingInternship := createRepositoryTestInternship(t, db, employerID, "Frontend Intern")
	acceptedInternship := createRepositoryTestInternship(t, db, employerID, "Mobile Intern")
	otherInternship := createRepositoryTestInternship(t, db, employerID, "Data Intern")

	createRepositoryTestApplication(t, db, submittedInternship.ID, student.ID, models.ApplicationStatusSubmitted, baseTime)
	newestPending := createRepositoryTestApplication(t, db, reviewingInternship.ID, student.ID, models.ApplicationStatusReviewing, baseTime.Add(time.Hour))
	createRepositoryTestApplication(t, db, acceptedInternship.ID, student.ID, models.ApplicationStatusAccepted, baseTime.Add(30*time.Minute))
	createRepositoryTestApplication(t, db, otherInternship.ID, otherStudent.ID, models.ApplicationStatusRejected, baseTime.Add(90*time.Minute))

	repo := NewInternshipApplicationRepository(db)
	items, total, err := repo.ListByStudent(context.Background(), StudentApplicationFilter{
		StudentID: student.ID,
		Statuses: []models.InternshipApplicationStatus{
			models.ApplicationStatusSubmitted,
			models.ApplicationStatusReviewing,
			models.ApplicationStatusShortlisted,
		},
		Page:     1,
		PageSize: 1,
	})
	if err != nil {
		t.Fatalf("list student applications: %v", err)
	}
	if total != 2 || len(items) != 1 {
		t.Fatalf("got total=%d len=%d, want first page of two pending applications", total, len(items))
	}
	if items[0].ID != newestPending.ID {
		t.Fatalf("first application = %s, want newest pending %s", items[0].ID, newestPending.ID)
	}
	if items[0].Internship == nil || items[0].Internship.Title != "Frontend Intern" {
		t.Fatalf("internship preload = %#v, want Frontend Intern", items[0].Internship)
	}

	accepted, acceptedTotal, err := repo.ListByStudent(context.Background(), StudentApplicationFilter{
		StudentID: student.ID,
		Statuses:  []models.InternshipApplicationStatus{models.ApplicationStatusAccepted},
		Page:      1,
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("list accepted student applications: %v", err)
	}
	if acceptedTotal != 1 || len(accepted) != 1 || accepted[0].Status != models.ApplicationStatusAccepted {
		t.Fatalf("accepted applications = %#v total=%d, want one accepted application", accepted, acceptedTotal)
	}
}

func openApplicationRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:recruiter-applications-%s?mode=memory&cache=shared", uuid.NewString())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := db.AutoMigrate(
		&models.StudentProfile{},
		&models.StudentDocument{},
		&models.RecruiterProfile{},
		&models.Internship{},
		&models.InternshipApplication{},
	); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return db
}

func createRepositoryTestInternship(t *testing.T, db *gorm.DB, employerID uuid.UUID, title string) *models.Internship {
	t.Helper()
	internship := &models.Internship{
		IssuedBy:       employerID,
		Title:          title,
		Description:    "Test role",
		WorkMode:       "remote",
		InternshipType: "paid",
		Status:         enums.InternshipStatusPublished,
		IsActive:       true,
	}
	if err := db.Omit("Issuer").Create(internship).Error; err != nil {
		t.Fatalf("create internship: %v", err)
	}
	return internship
}

func createRepositoryTestStudent(t *testing.T, db *gorm.DB, fullName string) *models.StudentProfile {
	t.Helper()
	student := &models.StudentProfile{UserID: uuid.New(), FullName: fullName}
	if err := db.Create(student).Error; err != nil {
		t.Fatalf("create student: %v", err)
	}
	return student
}

func createRepositoryTestApplication(
	t *testing.T,
	db *gorm.DB,
	internshipID, studentID uuid.UUID,
	status models.InternshipApplicationStatus,
	appliedAt time.Time,
) *models.InternshipApplication {
	t.Helper()
	application := &models.InternshipApplication{
		InternshipID: internshipID,
		StudentID:    studentID,
		Status:       status,
		AppliedAt:    appliedAt,
	}
	if err := db.Omit("Internship", "Student").Create(application).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	return application
}
