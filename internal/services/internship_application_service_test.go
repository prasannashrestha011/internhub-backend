package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

func TestIsValidEmployerStatusTransition(t *testing.T) {
	for _, current := range []models.InternshipApplicationStatus{models.ApplicationStatusSubmitted, models.ApplicationStatusAccepted, models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn} {
		for _, next := range []models.InternshipApplicationStatus{models.ApplicationStatusSubmitted, models.ApplicationStatusAccepted, models.ApplicationStatusRejected, models.ApplicationStatusWithdrawn, "reviewing", "shortlisted", "unknown"} {
			want := current == models.ApplicationStatusSubmitted && (next == models.ApplicationStatusAccepted || next == models.ApplicationStatusRejected)
			if got := isValidEmployerStatusTransition(current, next); got != want {
				t.Fatalf("transition %s -> %s = %v, want %v", current, next, got, want)
			}
		}
	}
}

type fakeDocumentURLSigner struct {
	url *url.URL
	err error
}

func (f fakeDocumentURLSigner) PresignedGetObject(
	context.Context,
	string,
	string,
	time.Duration,
	url.Values,
) (*url.URL, error) {
	return f.url, f.err
}

func TestGetForRecruiterDocumentAndOwnership(t *testing.T) {
	db := openApplicationServiceTestDB(t)
	employerID := uuid.New()
	otherEmployerID := uuid.New()
	internship := createServiceTestInternship(t, db, employerID, "Platform Intern")
	student := createServiceTestStudent(t, db, "Anita Gurung")
	application := createServiceTestApplication(t, db, internship.ID, student.ID, models.ApplicationStatusSubmitted, "")
	document := &models.StudentDocument{
		ProfileID: student.ID,
		UserID:    student.UserID,
		ObjectKey: "students/anita/resume.pdf",
		FileName:  "anita-resume.pdf",
		MimeType:  "application/pdf",
		Size:      2048,
	}
	if err := db.Create(document).Error; err != nil {
		t.Fatalf("create document: %v", err)
	}

	presigned, _ := url.Parse("https://minio.example.test/student-docs/anita-resume.pdf?signature=test")
	service := newApplicationServiceForTest(db, fakeDocumentURLSigner{url: presigned})
	detail, err := service.GetForRecruiter(context.Background(), application.ID, employerID)
	if err != nil {
		t.Fatalf("get recruiter application: %v", err)
	}
	if detail.Document == nil || detail.Document.FileName != document.FileName {
		t.Fatalf("document = %#v, want %s metadata", detail.Document, document.FileName)
	}
	if detail.DocumentURL == nil || *detail.DocumentURL != presigned.String() {
		t.Fatalf("document URL = %#v, want %s", detail.DocumentURL, presigned)
	}
	if detail.Application.Student == nil || detail.Application.Student.FullName != student.FullName {
		t.Fatalf("candidate = %#v, want %s", detail.Application.Student, student.FullName)
	}

	if _, err := service.GetForRecruiter(context.Background(), application.ID, otherEmployerID); !errors.Is(err, ErrUnauthorizedAccess) {
		t.Fatalf("other employer error = %v, want ErrUnauthorizedAccess", err)
	}
	if _, err := service.GetForRecruiter(context.Background(), uuid.New(), employerID); !errors.Is(err, ErrApplicationNotFound) {
		t.Fatalf("missing application error = %v, want ErrApplicationNotFound", err)
	}

	serviceWithSigningFailure := newApplicationServiceForTest(db, fakeDocumentURLSigner{err: errors.New("minio unavailable")})
	if _, err := serviceWithSigningFailure.GetForRecruiter(context.Background(), application.ID, employerID); err == nil {
		t.Fatal("expected document signing failure")
	}
}

func TestGetForRecruiterWithoutDocument(t *testing.T) {
	db := openApplicationServiceTestDB(t)
	employerID := uuid.New()
	internship := createServiceTestInternship(t, db, employerID, "QA Intern")
	student := createServiceTestStudent(t, db, "Suman Karki")
	application := createServiceTestApplication(t, db, internship.ID, student.ID, models.ApplicationStatusSubmitted, "")

	service := newApplicationServiceForTest(db, nil)
	detail, err := service.GetForRecruiter(context.Background(), application.ID, employerID)
	if err != nil {
		t.Fatalf("get application without document: %v", err)
	}
	if detail.Document != nil || detail.DocumentURL != nil {
		t.Fatalf("document fields = %#v, %#v; want nil", detail.Document, detail.DocumentURL)
	}
}

func TestUpdateStatusDirectDecisionsAndEmployerNote(t *testing.T) {
	db := openApplicationServiceTestDB(t)
	employerID := uuid.New()
	internship := createServiceTestInternship(t, db, employerID, "Mobile Intern")
	student := createServiceTestStudent(t, db, "Rita Thapa")
	service := newApplicationServiceForTest(db, nil)
	ctx := context.Background()

	acceptedApplication := createServiceTestApplication(t, db, internship.ID, student.ID, models.ApplicationStatusSubmitted, "")
	acceptNote := "  Welcome to the team.  "
	accepted, err := service.UpdateStatus(ctx, acceptedApplication.ID, employerID, models.ApplicationStatusAccepted, &acceptNote)
	if err != nil {
		t.Fatalf("accept submitted application: %v", err)
	}
	if accepted.Status != models.ApplicationStatusAccepted {
		t.Fatalf("accepted result = %#v, want accepted status", accepted)
	}
	if accepted.EmployerNote != "Welcome to the team." {
		t.Fatalf("accepted note = %q, want trimmed message", accepted.EmployerNote)
	}

	secondStudent := createServiceTestStudent(t, db, "Nima Sherpa")
	rejectedApplication := createServiceTestApplication(t, db, internship.ID, secondStudent.ID, models.ApplicationStatusSubmitted, "Existing message")
	rejected, err := service.UpdateStatus(ctx, rejectedApplication.ID, employerID, models.ApplicationStatusRejected, nil)
	if err != nil {
		t.Fatalf("reject submitted application: %v", err)
	}
	if rejected.Status != models.ApplicationStatusRejected || rejected.EmployerNote != "Existing message" {
		t.Fatalf("rejected result = %#v, want rejected status and preserved note", rejected)
	}

	thirdStudent := createServiceTestStudent(t, db, "Kabita Lama")
	clearedApplication := createServiceTestApplication(t, db, internship.ID, thirdStudent.ID, models.ApplicationStatusSubmitted, "Clear me")
	emptyNote := "   "
	cleared, err := service.UpdateStatus(ctx, clearedApplication.ID, employerID, models.ApplicationStatusRejected, &emptyNote)
	if err != nil {
		t.Fatalf("reject and clear note: %v", err)
	}
	if cleared.EmployerNote != "" {
		t.Fatalf("cleared note = %q, want empty", cleared.EmployerNote)
	}
}

func TestListForRecruiterValidatesFilters(t *testing.T) {
	db := openApplicationServiceTestDB(t)
	employerID := uuid.New()
	otherEmployerID := uuid.New()
	otherInternship := createServiceTestInternship(t, db, otherEmployerID, "Other Employer Role")
	service := newApplicationServiceForTest(db, nil)

	if _, _, err := service.ListForRecruiter(context.Background(), uuid.Nil, RecruiterApplicationFilter{}); !errors.Is(err, ErrInvalidApplicationData) {
		t.Fatalf("nil employer error = %v, want ErrInvalidApplicationData", err)
	}
	if _, _, err := service.ListForRecruiter(context.Background(), employerID, RecruiterApplicationFilter{Status: "unknown"}); !errors.Is(err, ErrInvalidApplicationData) {
		t.Fatalf("invalid status error = %v, want ErrInvalidApplicationData", err)
	}
	if _, _, err := service.ListForRecruiter(context.Background(), employerID, RecruiterApplicationFilter{InternshipID: &otherInternship.ID}); !errors.Is(err, ErrUnauthorizedAccess) {
		t.Fatalf("other internship error = %v, want ErrUnauthorizedAccess", err)
	}
}

func TestListByStudentFiltersAndMapsApplications(t *testing.T) {
	db := openApplicationServiceTestDB(t)
	employerID := uuid.New()
	if err := db.Create(&models.RecruiterProfile{
		UserID:           employerID,
		OrganizationName: "Acme Nepal",
	}).Error; err != nil {
		t.Fatalf("create recruiter profile: %v", err)
	}

	student := createServiceTestStudent(t, db, "Aarav Sharma")
	service := newApplicationServiceForTest(db, nil)
	statuses := []models.InternshipApplicationStatus{
		models.ApplicationStatusSubmitted,
		models.ApplicationStatusAccepted,
		models.ApplicationStatusRejected,
	}
	for index, status := range statuses {
		internship := createServiceTestInternship(t, db, employerID, fmt.Sprintf("Role %d", index+1))
		createServiceTestApplication(t, db, internship.ID, student.ID, status, "Student-visible decision")
	}

	pending, total, err := service.ListByStudent(context.Background(), student.UserID, StudentApplicationFilter{
		Status:   models.ApplicationStatusSubmitted,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list pending student applications: %v", err)
	}
	if total != 1 || len(pending) != 1 {
		t.Fatalf("pending total=%d len=%d, want one submitted application", total, len(pending))
	}
	for _, application := range pending {
		if application.Status != models.ApplicationStatusSubmitted {
			t.Fatalf("pending filter returned terminal status %q", application.Status)
		}
		if application.Internship == nil || application.Internship.OrganizationName != "Acme Nepal" {
			t.Fatalf("internship summary = %#v, want organization name", application.Internship)
		}
		if application.EmployerNote != "Student-visible decision" {
			t.Fatalf("employer note = %q, want student-visible decision", application.EmployerNote)
		}
	}

	accepted, acceptedTotal, err := service.ListByStudent(context.Background(), student.UserID, StudentApplicationFilter{
		Status:   models.ApplicationStatusAccepted,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("list accepted student applications: %v", err)
	}
	if acceptedTotal != 1 || len(accepted) != 1 || accepted[0].Status != models.ApplicationStatusAccepted {
		t.Fatalf("accepted applications = %#v total=%d, want one accepted application", accepted, acceptedTotal)
	}

	if _, _, err := service.ListByStudent(context.Background(), student.UserID, StudentApplicationFilter{Status: "unknown"}); !errors.Is(err, ErrInvalidApplicationData) {
		t.Fatalf("invalid filter error = %v, want ErrInvalidApplicationData", err)
	}
	if _, _, err := service.ListByStudent(context.Background(), uuid.Nil, StudentApplicationFilter{}); !errors.Is(err, ErrInvalidApplicationData) {
		t.Fatalf("nil user error = %v, want ErrInvalidApplicationData", err)
	}
}

func TestFindByStudentAndInternship(t *testing.T) {
	db := openApplicationServiceTestDB(t)
	employerID := uuid.New()
	student := createServiceTestStudent(t, db, "Aarav Sharma")
	internship := createServiceTestInternship(t, db, employerID, "Frontend Intern")
	created := createServiceTestApplication(
		t,
		db,
		internship.ID,
		student.ID,
		models.ApplicationStatusWithdrawn,
		"",
	)
	service := newApplicationServiceForTest(db, nil)

	found, err := service.FindByStudentAndInternship(context.Background(), student.UserID, internship.ID)
	if err != nil {
		t.Fatalf("find existing application: %v", err)
	}
	if found == nil || found.ID != created.ID || found.Status != models.ApplicationStatusWithdrawn {
		t.Fatalf("application = %#v, want withdrawn application %s", found, created.ID)
	}

	notApplied, err := service.FindByStudentAndInternship(context.Background(), student.UserID, uuid.New())
	if err != nil {
		t.Fatalf("find missing application: %v", err)
	}
	if notApplied != nil {
		t.Fatalf("missing application = %#v, want nil", notApplied)
	}

	if _, err := service.FindByStudentAndInternship(context.Background(), uuid.Nil, internship.ID); !errors.Is(err, ErrInvalidApplicationData) {
		t.Fatalf("invalid lookup error = %v, want ErrInvalidApplicationData", err)
	}
}

func TestCreateApplicationRejectsDuplicate(t *testing.T) {
	db := openApplicationServiceTestDB(t)
	student := createServiceTestStudent(t, db, "Maya Gurung")
	employerID := uuid.New()
	if err := db.Create(&models.RecruiterProfile{UserID: employerID, OrganizationName: "Employer"}).Error; err != nil {
		t.Fatal(err)
	}
	internship := createServiceTestInternship(t, db, employerID, "Backend Intern")
	createServiceTestApplication(
		t,
		db,
		internship.ID,
		student.ID,
		models.ApplicationStatusWithdrawn,
		"",
	)
	service := newApplicationServiceForTest(db, nil)

	err := service.CreateApplication(context.Background(), student.UserID, &models.InternshipApplication{
		InternshipID: internship.ID,
		StudentID:    student.UserID,
	})
	if !errors.Is(err, ErrAlreadyApplied) {
		t.Fatalf("duplicate application error = %v, want ErrAlreadyApplied", err)
	}
}

func openApplicationServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:application-service-%s?mode=memory&cache=shared", uuid.NewString())
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

func newApplicationServiceForTest(db *gorm.DB, signer documentURLSigner) *InternshipApplicationService {
	return NewInternshipApplicationService(
		repositories.NewInternshipApplicationRepository(db),
		repositories.NewInternshipRepository(db),
		repositories.NewStudentRepository(db),
		signer,
		"student-documents",
		&RecruiterProfileService{},
		repositories.NewUserRepository(db),
		nil,
		nil,
	)
}

func createServiceTestInternship(t *testing.T, db *gorm.DB, employerID uuid.UUID, title string) *models.Internship {
	t.Helper()
	internship := &models.Internship{
		IssuedBy:       employerID,
		Title:          title,
		Description:    "Test role",
		WorkMode:       "remote",
		InternshipType: "paid",
		Status:         enums.InternshipStatusPublished,
	}
	if err := db.Omit("Issuer").Create(internship).Error; err != nil {
		t.Fatalf("create internship: %v", err)
	}
	return internship
}

func createServiceTestStudent(t *testing.T, db *gorm.DB, fullName string) *models.StudentProfile {
	t.Helper()
	student := &models.StudentProfile{
		UserID:         uuid.New(),
		FullName:       fullName,
		CollegeName:    "Tribhuvan University",
		Degree:         "BSc CSIT",
		FacultyOrMajor: "Computer Science",
	}
	if err := db.Create(student).Error; err != nil {
		t.Fatalf("create student: %v", err)
	}
	return student
}

func createServiceTestApplication(
	t *testing.T,
	db *gorm.DB,
	internshipID, studentID uuid.UUID,
	status models.InternshipApplicationStatus,
	employerNote string,
) *models.InternshipApplication {
	t.Helper()
	application := &models.InternshipApplication{
		InternshipID: internshipID,
		StudentID:    studentID,
		Status:       status,
		EmployerNote: employerNote,
		AppliedAt:    time.Now(),
	}
	if err := db.Omit("Internship", "Student").Create(application).Error; err != nil {
		t.Fatalf("create application: %v", err)
	}
	return application
}

func TestIsWithdrawableApplicationStatus(t *testing.T) {
	tests := []struct {
		name   string
		status models.InternshipApplicationStatus
		want   bool
	}{
		{"submitted application", models.ApplicationStatusSubmitted, true},
		{"accepted application is final", models.ApplicationStatusAccepted, false},
		{"rejected application is final", models.ApplicationStatusRejected, false},
		{"withdrawn application is already final", models.ApplicationStatusWithdrawn, false},
		{"unknown status", models.InternshipApplicationStatus("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWithdrawableApplicationStatus(tt.status); got != tt.want {
				t.Fatalf("isWithdrawableApplicationStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

type applicationStatusSenderFunc func(context.Context, string, string, models.InternshipApplicationStatus, string) error

func (f applicationStatusSenderFunc) SendApplicationStatus(ctx context.Context, to, title string, status models.InternshipApplicationStatus, note string) error {
	return f(ctx, to, title, status, note)
}

func TestUpdateStatusNotifiesStudentAfterSaving(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       models.InternshipApplicationStatus
		failDelivery bool
	}{
		{"acceptance", models.ApplicationStatusAccepted, false},
		{"rejection", models.ApplicationStatusRejected, false},
		{"delivery failure preserves decision", models.ApplicationStatusAccepted, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openApplicationServiceTestDB(t)
			if err := db.AutoMigrate(&models.User{}); err != nil {
				t.Fatal(err)
			}
			employerID := uuid.New()
			internship := createServiceTestInternship(t, db, employerID, "Engineering Intern")
			student := createServiceTestStudent(t, db, "Student")
			if err := db.Create(&models.User{ID: student.UserID, Email: "student@example.com", PasswordHash: "test", Role: models.RoleStudent}).Error; err != nil {
				t.Fatal(err)
			}
			application := createServiceTestApplication(t, db, internship.ID, student.ID, models.ApplicationStatusSubmitted, "")
			service := newApplicationServiceForTest(db, nil)
			calls := 0
			service.sender = applicationStatusSenderFunc(func(ctx context.Context, to, title string, status models.InternshipApplicationStatus, note string) error {
				calls++
				if to != "student@example.com" || title != internship.Title || status != tc.status || note != "Recruiter message" {
					t.Fatalf("unexpected email: %q %q %q %q", to, title, status, note)
				}
				saved, err := service.repo.GetByID(ctx, application.ID)
				if err != nil || saved.Status != status || saved.EmployerNote != note {
					t.Fatalf("email sent before decision persisted: %#v, %v", saved, err)
				}
				if tc.failDelivery {
					return errors.New("mail unavailable")
				}
				return nil
			})
			note := "  Recruiter message  "
			ctx := context.Background()
			if _, err := service.UpdateStatus(ctx, application.ID, uuid.New(), tc.status, &note); !errors.Is(err, ErrUnauthorizedAccess) || calls != 0 {
				t.Fatalf("unauthorized decision: error=%v, email calls=%d", err, calls)
			}
			updated, err := service.UpdateStatus(ctx, application.ID, employerID, tc.status, &note)
			if err != nil || updated.Status != tc.status || calls != 1 {
				t.Fatalf("decision: result=%#v, error=%v, email calls=%d", updated, err, calls)
			}
			if _, err := service.UpdateStatus(ctx, application.ID, employerID, tc.status, &note); !errors.Is(err, ErrInvalidStatusTransition) || calls != 1 {
				t.Fatalf("repeated decision: error=%v, email calls=%d", err, calls)
			}
			own, err := service.FindByStudentAndInternship(ctx, student.UserID, internship.ID)
			if err != nil || own.Status != tc.status || own.EmployerNote != "Recruiter message" {
				t.Fatalf("student decision: result=%#v, error=%v", own, err)
			}
		})
	}
}
