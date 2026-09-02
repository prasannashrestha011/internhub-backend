package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

var (
	ErrInvalidApplicationData  = errors.New("invalid application data")
	ErrApplicationNotFound     = errors.New("application not found")
	ErrAlreadyApplied          = errors.New("student has already applied to this internship")
	ErrInternshipNotActive     = errors.New("internship is not active or published")
	ErrApplicationClosed       = errors.New("application deadline has passed")
	ErrUnauthorizedAccess      = errors.New("unauthorized access to application")
	ErrInvalidStatusTransition = errors.New("invalid status transition")
)

type InternshipApplicationService struct {
	repo           *repositories.InternshipApplicationRepository
	internshipRepo *repositories.InternshipRepository
	studentRepo    *repositories.StudentRepository
	minio          documentURLSigner
	bucketName     string
}

type documentURLSigner interface {
	PresignedGetObject(
		ctx context.Context,
		bucketName string,
		objectName string,
		expiry time.Duration,
		reqParams url.Values,
	) (*url.URL, error)
}

type RecruiterApplicationFilter struct {
	Query        string
	InternshipID *uuid.UUID
	Status       models.InternshipApplicationStatus
	Page         int
	PageSize     int
}

type StudentApplicationStatusFilter string

const (
	StudentApplicationFilterPending  StudentApplicationStatusFilter = "pending"
	StudentApplicationFilterAccepted StudentApplicationStatusFilter = "accepted"
	StudentApplicationFilterRejected StudentApplicationStatusFilter = "rejected"
)

type StudentApplicationFilter struct {
	Status   StudentApplicationStatusFilter
	Page     int
	PageSize int
}

type RecruiterCandidate struct {
	ID              uuid.UUID `json:"id"`
	FullName        string    `json:"full_name"`
	Phone           string    `json:"phone"`
	Location        string    `json:"location"`
	Bio             string    `json:"bio"`
	CollegeName     string    `json:"college_name"`
	Degree          string    `json:"degree"`
	FacultyOrMajor  string    `json:"faculty_or_major"`
	CurrentSemester string    `json:"current_semester"`
	GraduationYear  int       `json:"graduation_year"`
	Availability    string    `json:"availability"`
	LinkedinURL     string    `json:"linkedin_url"`
	GithubURL       string    `json:"github_url"`
	PortfolioURL    string    `json:"portfolio_url"`
}

type RecruiterCandidateSummary struct {
	ID              uuid.UUID `json:"id"`
	FullName        string    `json:"full_name"`
	Location        string    `json:"location"`
	CollegeName     string    `json:"college_name"`
	Degree          string    `json:"degree"`
	FacultyOrMajor  string    `json:"faculty_or_major"`
	CurrentSemester string    `json:"current_semester"`
}

type RecruiterInternshipSummary struct {
	ID             uuid.UUID              `json:"id"`
	Title          string                 `json:"title"`
	Location       string                 `json:"location"`
	WorkMode       string                 `json:"work_mode"`
	InternshipType string                 `json:"internship_type"`
	Status         enums.InternshipStatus `json:"status"`
}

type RecruiterApplication struct {
	ID            uuid.UUID                          `json:"id"`
	InternshipID  uuid.UUID                          `json:"internship_id"`
	StudentID     uuid.UUID                          `json:"student_id"`
	Status        models.InternshipApplicationStatus `json:"status"`
	EmployerNote  string                             `json:"employer_note"`
	AppliedAt     time.Time                          `json:"applied_at"`
	ReviewedAt    *time.Time                         `json:"reviewed_at"`
	ShortlistedAt *time.Time                         `json:"shortlisted_at"`
	AcceptedAt    *time.Time                         `json:"accepted_at"`
	RejectedAt    *time.Time                         `json:"rejected_at"`
	WithdrawnAt   *time.Time                         `json:"withdrawn_at"`
	CreatedAt     time.Time                          `json:"created_at"`
	UpdatedAt     time.Time                          `json:"updated_at"`
	Student       *RecruiterCandidate                `json:"student"`
	Internship    *RecruiterInternshipSummary        `json:"internship"`
}

type RecruiterApplicationSummary struct {
	ID           uuid.UUID                          `json:"id"`
	InternshipID uuid.UUID                          `json:"internship_id"`
	StudentID    uuid.UUID                          `json:"student_id"`
	Status       models.InternshipApplicationStatus `json:"status"`
	AppliedAt    time.Time                          `json:"applied_at"`
	CreatedAt    time.Time                          `json:"created_at"`
	UpdatedAt    time.Time                          `json:"updated_at"`
	Student      *RecruiterCandidateSummary         `json:"student"`
	Internship   *RecruiterInternshipSummary        `json:"internship"`
}

type RecruiterApplicationDocument struct {
	ID       uuid.UUID `json:"id"`
	FileName string    `json:"file_name"`
	MimeType string    `json:"mime_type"`
	Size     int64     `json:"size"`
}

type RecruiterApplicationDetail struct {
	Application RecruiterApplication          `json:"application"`
	Document    *RecruiterApplicationDocument `json:"document"`
	DocumentURL *string                       `json:"document_url"`
}

type StudentApplicationInternship struct {
	ID               uuid.UUID `json:"id"`
	Title            string    `json:"title"`
	OrganizationName string    `json:"organization_name"`
	Location         string    `json:"location"`
	WorkMode         string    `json:"work_mode"`
	InternshipType   string    `json:"internship_type"`
	Duration         int       `json:"duration"`
	DurationUnit     string    `json:"duration_unit"`
}

type StudentApplicationSummary struct {
	ID            uuid.UUID                          `json:"id"`
	InternshipID  uuid.UUID                          `json:"internship_id"`
	Status        models.InternshipApplicationStatus `json:"status"`
	EmployerNote  string                             `json:"employer_note"`
	AppliedAt     time.Time                          `json:"applied_at"`
	ReviewedAt    *time.Time                         `json:"reviewed_at"`
	ShortlistedAt *time.Time                         `json:"shortlisted_at"`
	AcceptedAt    *time.Time                         `json:"accepted_at"`
	RejectedAt    *time.Time                         `json:"rejected_at"`
	WithdrawnAt   *time.Time                         `json:"withdrawn_at"`
	UpdatedAt     time.Time                          `json:"updated_at"`
	Internship    *StudentApplicationInternship      `json:"internship"`
}

func NewInternshipApplicationService(
	repo *repositories.InternshipApplicationRepository,
	internshipRepo *repositories.InternshipRepository,
	studentRepo *repositories.StudentRepository,
	minio documentURLSigner,
	bucketName string,
) *InternshipApplicationService {
	return &InternshipApplicationService{
		repo:           repo,
		internshipRepo: internshipRepo,
		studentRepo:    studentRepo,
		minio:          minio,
		bucketName:     bucketName,
	}
}

func (s *InternshipApplicationService) CreateApplication(ctx context.Context, application *models.InternshipApplication) error {
	if application == nil {
		return fmt.Errorf("%w: application cannot be nil", ErrInvalidApplicationData)
	}
	if application.StudentID == uuid.Nil {
		return fmt.Errorf("%w: student ID is required", ErrInvalidApplicationData)
	}
	if application.InternshipID == uuid.Nil {
		return fmt.Errorf("%w: internship ID is required", ErrInvalidApplicationData)
	}

	// Verify student profile exists
	studentID, err := s.studentRepo.ResolveProfileID(application.StudentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: student profile not found", ErrInvalidApplicationData)
		}
		return fmt.Errorf("check student profile: %w", err)
	}
	application.StudentID = studentID

	// Check internship existence and eligibility
	internship, err := s.internshipRepo.GetByID(ctx, application.InternshipID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInternshipNotFound
		}
		return fmt.Errorf("check internship: %w", err)
	}

	if !internship.IsActive || internship.Status != enums.InternshipStatusPublished {
		return ErrInternshipNotActive
	}

	if internship.ApplicationDeadline != nil && time.Now().After(*internship.ApplicationDeadline) {
		return ErrApplicationClosed
	}

	// Check if student already applied
	existing, err := s.repo.GetByStudentAndInternship(ctx, application.StudentID, application.InternshipID)
	if err == nil && existing != nil {
		return ErrAlreadyApplied
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("check existing application: %w", err)
	}

	application.Status = models.ApplicationStatusSubmitted
	application.AppliedAt = time.Now()

	return s.repo.Create(ctx, application)
}

func (s *InternshipApplicationService) GetApplication(
	ctx context.Context,
	id uuid.UUID,
) (*models.InternshipApplication, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf(
			"%w: invalid application ID",
			ErrInvalidApplicationData,
		)
	}

	application, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrApplicationNotFound
	}
	if err != nil {
		return nil, err
	}

	return application, nil
}

func (s *InternshipApplicationService) ListByStudent(
	ctx context.Context,
	userID uuid.UUID,
	filter StudentApplicationFilter,
) ([]StudentApplicationSummary, int64, error) {
	if userID == uuid.Nil {
		return nil, 0, fmt.Errorf(
			"%w: invalid user ID",
			ErrInvalidApplicationData,
		)
	}

	studentID, err := s.studentRepo.ResolveProfileID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, fmt.Errorf(
				"%w: student profile not found",
				ErrInvalidApplicationData,
			)
		}
		return nil, 0, fmt.Errorf("resolve student profile: %w", err)
	}
	statuses, err := statusesForStudentApplicationFilter(filter.Status)
	if err != nil {
		return nil, 0, err
	}

	apps, total, err := s.repo.ListByStudent(
		ctx,
		repositories.StudentApplicationFilter{
			StudentID: studentID,
			Statuses:  statuses,
			Page:      filter.Page,
			PageSize:  filter.PageSize,
		},
	)
	if err != nil {
		return nil, 0, err
	}

	return mapStudentApplications(apps), total, nil
}

// FindByStudentAndInternship returns the authenticated student's application
// for one internship. A nil result means the student has never applied.
func (s *InternshipApplicationService) FindByStudentAndInternship(
	ctx context.Context,
	userID, internshipID uuid.UUID,
) (*StudentApplicationSummary, error) {
	if userID == uuid.Nil || internshipID == uuid.Nil {
		return nil, fmt.Errorf("%w: valid user and internship IDs are required", ErrInvalidApplicationData)
	}

	studentID, err := s.studentRepo.ResolveProfileID(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: student profile not found", ErrInvalidApplicationData)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve student profile: %w", err)
	}

	application, err := s.repo.GetByStudentAndInternship(ctx, studentID, internshipID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find student application: %w", err)
	}

	mapped := mapStudentApplications([]models.InternshipApplication{*application})
	return &mapped[0], nil
}

func (s *InternshipApplicationService) ListByInternship(ctx context.Context, internshipID, employerID uuid.UUID, page, pageSize int) ([]RecruiterApplicationSummary, int64, error) {
	return s.ListForRecruiter(ctx, employerID, RecruiterApplicationFilter{
		InternshipID: &internshipID,
		Page:         page,
		PageSize:     pageSize,
	})
}

func (s *InternshipApplicationService) ListForRecruiter(
	ctx context.Context,
	employerID uuid.UUID,
	filter RecruiterApplicationFilter,
) ([]RecruiterApplicationSummary, int64, error) {
	if employerID == uuid.Nil {
		return nil, 0, fmt.Errorf("%w: invalid employer ID", ErrInvalidApplicationData)
	}
	if filter.InternshipID != nil {
		if *filter.InternshipID == uuid.Nil {
			return nil, 0, fmt.Errorf("%w: invalid internship ID", ErrInvalidApplicationData)
		}
		internship, err := s.internshipRepo.GetByID(ctx, *filter.InternshipID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, ErrInternshipNotFound
		}
		if err != nil {
			return nil, 0, fmt.Errorf("check internship ownership: %w", err)
		}
		if internship.IssuedBy != employerID {
			return nil, 0, ErrUnauthorizedAccess
		}
	}
	if filter.Status != "" && !isKnownApplicationStatus(filter.Status) {
		return nil, 0, fmt.Errorf("%w: invalid application status", ErrInvalidApplicationData)
	}

	apps, total, err := s.repo.ListForEmployer(ctx, repositories.RecruiterApplicationFilter{
		EmployerID:   employerID,
		Query:        strings.TrimSpace(filter.Query),
		InternshipID: filter.InternshipID,
		Status:       filter.Status,
		Page:         filter.Page,
		PageSize:     filter.PageSize,
	})
	if err != nil {
		return nil, 0, err
	}

	return mapRecruiterApplications(apps), total, nil
}

func (s *InternshipApplicationService) GetForRecruiter(
	ctx context.Context,
	applicationID, employerID uuid.UUID,
) (*RecruiterApplicationDetail, error) {
	if applicationID == uuid.Nil || employerID == uuid.Nil {
		return nil, fmt.Errorf("%w: application ID and employer ID are required", ErrInvalidApplicationData)
	}

	application, err := s.GetApplication(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	if application.Internship == nil || application.Internship.IssuedBy != employerID {
		return nil, ErrUnauthorizedAccess
	}

	detail := &RecruiterApplicationDetail{
		Application: mapRecruiterApplication(application),
	}
	if application.Student == nil || len(application.Student.Documents) == 0 {
		return detail, nil
	}

	document := application.Student.Documents[0]
	detail.Document = &RecruiterApplicationDocument{
		ID:       document.ID,
		FileName: document.FileName,
		MimeType: document.MimeType,
		Size:     document.Size,
	}
	if document.ObjectKey == "" {
		return detail, nil
	}
	if s.minio == nil {
		return nil, errors.New("generate presigned URL: document storage is unavailable")
	}

	presignedURL, err := s.minio.PresignedGetObject(
		ctx,
		s.bucketName,
		document.ObjectKey,
		time.Hour,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("generate presigned URL: %w", err)
	}
	documentURL := presignedURL.String()
	detail.DocumentURL = &documentURL

	return detail, nil
}

func (s *InternshipApplicationService) UpdateStatus(
	ctx context.Context,
	applicationID, employerID uuid.UUID,
	newStatus models.InternshipApplicationStatus,
	employerNote *string,
) (*RecruiterApplication, error) {
	if applicationID == uuid.Nil || employerID == uuid.Nil {
		return nil, fmt.Errorf("%w: application ID and employer ID are required", ErrInvalidApplicationData)
	}

	application, err := s.GetApplication(ctx, applicationID)
	if err != nil {
		return nil, err
	}

	// Verify employer owns the internship being applied for
	internship, err := s.internshipRepo.GetByID(ctx, application.InternshipID)
	if err != nil {
		return nil, fmt.Errorf("check internship ownership: %w", err)
	}

	if internship.IssuedBy != employerID {
		return nil, ErrUnauthorizedAccess
	}
	if !isValidEmployerStatusTransition(application.Status, newStatus) {
		return nil, fmt.Errorf(
			"%w: cannot change status from %s to %s",
			ErrInvalidStatusTransition,
			application.Status,
			newStatus,
		)
	}

	now := time.Now()
	statusChanged := application.Status != newStatus
	application.Status = newStatus
	if employerNote != nil {
		application.EmployerNote = strings.TrimSpace(*employerNote)
	}

	switch {
	case statusChanged && newStatus == models.ApplicationStatusReviewing:
		application.ReviewedAt = &now
	case statusChanged && newStatus == models.ApplicationStatusShortlisted:
		application.ShortlistedAt = &now
	case statusChanged && newStatus == models.ApplicationStatusAccepted:
		application.AcceptedAt = &now
	case statusChanged && newStatus == models.ApplicationStatusRejected:
		application.RejectedAt = &now
	}

	if err := s.repo.Update(ctx, application); err != nil {
		return nil, err
	}

	updated := mapRecruiterApplication(application)
	return &updated, nil
}

// isValidEmployerStatusTransition keeps the application workflow small and
// prevents employers from setting student-only or unknown statuses.
func isValidEmployerStatusTransition(current, next models.InternshipApplicationStatus) bool {
	if current == next {
		switch next {
		case models.ApplicationStatusReviewing,
			models.ApplicationStatusShortlisted:
			return true
		default:
			return false
		}
	}

	switch current {
	case models.ApplicationStatusSubmitted:
		return next == models.ApplicationStatusReviewing ||
			next == models.ApplicationStatusShortlisted ||
			next == models.ApplicationStatusAccepted ||
			next == models.ApplicationStatusRejected
	case models.ApplicationStatusReviewing:
		return next == models.ApplicationStatusShortlisted ||
			next == models.ApplicationStatusAccepted ||
			next == models.ApplicationStatusRejected
	case models.ApplicationStatusShortlisted:
		return next == models.ApplicationStatusAccepted ||
			next == models.ApplicationStatusRejected
	default:
		return false
	}
}

func (s *InternshipApplicationService) WithdrawApplication(ctx context.Context, applicationID, studentProfileID uuid.UUID) error {
	if applicationID == uuid.Nil || studentProfileID == uuid.Nil {
		return fmt.Errorf("%w: application ID and student profile ID are required", ErrInvalidApplicationData)
	}

	application, err := s.GetApplication(ctx, applicationID)
	if err != nil {
		return err
	}

	if application.StudentID != studentProfileID {
		return ErrUnauthorizedAccess
	}

	if application.Status == models.ApplicationStatusWithdrawn {
		return nil
	}
	if !isWithdrawableApplicationStatus(application.Status) {
		return fmt.Errorf(
			"%w: application with status %s cannot be withdrawn",
			ErrInvalidStatusTransition,
			application.Status,
		)
	}

	now := time.Now()
	application.Status = models.ApplicationStatusWithdrawn
	application.WithdrawnAt = &now

	return s.repo.Update(ctx, application)
}

func isWithdrawableApplicationStatus(status models.InternshipApplicationStatus) bool {
	switch status {
	case models.ApplicationStatusSubmitted,
		models.ApplicationStatusReviewing,
		models.ApplicationStatusShortlisted:
		return true
	default:
		return false
	}
}

func isKnownApplicationStatus(status models.InternshipApplicationStatus) bool {
	switch status {
	case models.ApplicationStatusSubmitted,
		models.ApplicationStatusReviewing,
		models.ApplicationStatusShortlisted,
		models.ApplicationStatusRejected,
		models.ApplicationStatusAccepted,
		models.ApplicationStatusWithdrawn:
		return true
	default:
		return false
	}
}

func statusesForStudentApplicationFilter(
	filter StudentApplicationStatusFilter,
) ([]models.InternshipApplicationStatus, error) {
	switch filter {
	case "":
		return nil, nil
	case StudentApplicationFilterPending:
		return []models.InternshipApplicationStatus{
			models.ApplicationStatusSubmitted,
			models.ApplicationStatusReviewing,
			models.ApplicationStatusShortlisted,
		}, nil
	case StudentApplicationFilterAccepted:
		return []models.InternshipApplicationStatus{models.ApplicationStatusAccepted}, nil
	case StudentApplicationFilterRejected:
		return []models.InternshipApplicationStatus{models.ApplicationStatusRejected}, nil
	default:
		return nil, fmt.Errorf("%w: invalid student application status filter", ErrInvalidApplicationData)
	}
}

func mapStudentApplications(applications []models.InternshipApplication) []StudentApplicationSummary {
	mapped := make([]StudentApplicationSummary, len(applications))
	for i := range applications {
		application := &applications[i]
		mapped[i] = StudentApplicationSummary{
			ID:            application.ID,
			InternshipID:  application.InternshipID,
			Status:        application.Status,
			EmployerNote:  application.EmployerNote,
			AppliedAt:     application.AppliedAt,
			ReviewedAt:    application.ReviewedAt,
			ShortlistedAt: application.ShortlistedAt,
			AcceptedAt:    application.AcceptedAt,
			RejectedAt:    application.RejectedAt,
			WithdrawnAt:   application.WithdrawnAt,
			UpdatedAt:     application.UpdatedAt,
		}
		if application.Internship != nil {
			mapped[i].Internship = &StudentApplicationInternship{
				ID:             application.Internship.ID,
				Title:          application.Internship.Title,
				Location:       application.Internship.Location,
				WorkMode:       application.Internship.WorkMode,
				InternshipType: application.Internship.InternshipType,
				Duration:       application.Internship.Duration,
				DurationUnit:   application.Internship.DurationUnit,
			}
			mapped[i].Internship.OrganizationName = application.Internship.Issuer.OrganizationName
		}
	}
	return mapped
}

func mapRecruiterApplications(applications []models.InternshipApplication) []RecruiterApplicationSummary {
	mapped := make([]RecruiterApplicationSummary, len(applications))
	for i := range applications {
		application := &applications[i]
		mapped[i] = RecruiterApplicationSummary{
			ID:           application.ID,
			InternshipID: application.InternshipID,
			StudentID:    application.StudentID,
			Status:       application.Status,
			AppliedAt:    application.AppliedAt,
			CreatedAt:    application.CreatedAt,
			UpdatedAt:    application.UpdatedAt,
		}
		if application.Student != nil {
			mapped[i].Student = &RecruiterCandidateSummary{
				ID:              application.Student.ID,
				FullName:        application.Student.FullName,
				Location:        application.Student.Location,
				CollegeName:     application.Student.CollegeName,
				Degree:          application.Student.Degree,
				FacultyOrMajor:  application.Student.FacultyOrMajor,
				CurrentSemester: application.Student.CurrentSemester,
			}
		}
		if application.Internship != nil {
			mapped[i].Internship = &RecruiterInternshipSummary{
				ID:             application.Internship.ID,
				Title:          application.Internship.Title,
				Location:       application.Internship.Location,
				WorkMode:       application.Internship.WorkMode,
				InternshipType: application.Internship.InternshipType,
				Status:         application.Internship.Status,
			}
		}
	}
	return mapped
}

func mapRecruiterApplication(application *models.InternshipApplication) RecruiterApplication {
	mapped := RecruiterApplication{
		ID:            application.ID,
		InternshipID:  application.InternshipID,
		StudentID:     application.StudentID,
		Status:        application.Status,
		EmployerNote:  application.EmployerNote,
		AppliedAt:     application.AppliedAt,
		ReviewedAt:    application.ReviewedAt,
		ShortlistedAt: application.ShortlistedAt,
		AcceptedAt:    application.AcceptedAt,
		RejectedAt:    application.RejectedAt,
		WithdrawnAt:   application.WithdrawnAt,
		CreatedAt:     application.CreatedAt,
		UpdatedAt:     application.UpdatedAt,
	}
	if application.Student != nil {
		mapped.Student = &RecruiterCandidate{
			ID:              application.Student.ID,
			FullName:        application.Student.FullName,
			Phone:           application.Student.Phone,
			Location:        application.Student.Location,
			Bio:             application.Student.Bio,
			CollegeName:     application.Student.CollegeName,
			Degree:          application.Student.Degree,
			FacultyOrMajor:  application.Student.FacultyOrMajor,
			CurrentSemester: application.Student.CurrentSemester,
			GraduationYear:  application.Student.GraduationYear,
			Availability:    application.Student.Availability,
			LinkedinURL:     application.Student.LinkedinURL,
			GithubURL:       application.Student.GithubURL,
			PortfolioURL:    application.Student.PortfolioURL,
		}
	}
	if application.Internship != nil {
		mapped.Internship = &RecruiterInternshipSummary{
			ID:             application.Internship.ID,
			Title:          application.Internship.Title,
			Location:       application.Internship.Location,
			WorkMode:       application.Internship.WorkMode,
			InternshipType: application.Internship.InternshipType,
			Status:         application.Internship.Status,
		}
	}
	return mapped
}
