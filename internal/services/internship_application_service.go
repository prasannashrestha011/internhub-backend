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
	"github.com/prasanna/student-job-portal/backend/internal/logger"
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
	users          *repositories.UserRepository
	sender         ApplicationStatusSender
	log            *logger.Logger
	profiles       *RecruiterProfileService
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

type StudentApplicationFilter struct {
	Status   models.InternshipApplicationStatus
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
	ID           uuid.UUID                          `json:"id"`
	InternshipID uuid.UUID                          `json:"internship_id"`
	StudentID    uuid.UUID                          `json:"student_id"`
	Status       models.InternshipApplicationStatus `json:"status"`
	EmployerNote string                             `json:"employer_note"`
	AppliedAt    time.Time                          `json:"applied_at"`
	CreatedAt    time.Time                          `json:"created_at"`
	UpdatedAt    time.Time                          `json:"updated_at"`
	Student      *RecruiterCandidate                `json:"student"`
	Internship   *RecruiterInternshipSummary        `json:"internship"`
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
	OrganizationLogo string    `json:"organization_logo,omitempty"`
	Location         string    `json:"location"`
	WorkMode         string    `json:"work_mode"`
	InternshipType   string    `json:"internship_type"`
	Duration         int       `json:"duration"`
	DurationUnit     string    `json:"duration_unit"`
}

type StudentApplicationSummary struct {
	ID           uuid.UUID                          `json:"id"`
	InternshipID uuid.UUID                          `json:"internship_id"`
	Status       models.InternshipApplicationStatus `json:"status"`
	EmployerNote string                             `json:"employer_note"`
	AppliedAt    time.Time                          `json:"applied_at"`
	UpdatedAt    time.Time                          `json:"updated_at"`
	Internship   *StudentApplicationInternship      `json:"internship"`
}

func NewInternshipApplicationService(
	repo *repositories.InternshipApplicationRepository,
	internshipRepo *repositories.InternshipRepository,
	studentRepo *repositories.StudentRepository,
	minio documentURLSigner,
	bucketName string,
	profiles *RecruiterProfileService,
	users *repositories.UserRepository,
	sender ApplicationStatusSender,
	log *logger.Logger,
) *InternshipApplicationService {
	return &InternshipApplicationService{
		users:          users,
		sender:         sender,
		log:            log,
		profiles:       profiles,
		repo:           repo,
		internshipRepo: internshipRepo,
		studentRepo:    studentRepo,
		minio:          minio,
		bucketName:     bucketName,
	}
}

func (s *InternshipApplicationService) CreateApplication(ctx context.Context, userID uuid.UUID, application *models.InternshipApplication) error {
	if application == nil {
		return fmt.Errorf("%w: application cannot be nil", ErrInvalidApplicationData)
	}
	if userID == uuid.Nil {
		return fmt.Errorf("%w: student ID is required", ErrInvalidApplicationData)
	}
	if application.InternshipID == uuid.Nil {
		return fmt.Errorf("%w: internship ID is required", ErrInvalidApplicationData)
	}

	// Verify student profile exists
	student, err := s.studentRepo.GetByUserIDContext(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: student profile not found", ErrInvalidApplicationData)
		}
		return fmt.Errorf("check student profile: %w", err)
	}

	application.StudentID = student.ID

	// Check internship existence and eligibility
	internship, err := s.internshipRepo.GetByID(ctx, application.InternshipID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInternshipNotFound
		}
		return fmt.Errorf("check internship: %w", err)
	}

	if internship.Status != enums.InternshipStatusPublished {
		return ErrInternshipNotActive
	}

	if internship.ApplicationDeadline != nil && time.Now().After(*internship.ApplicationDeadline) {
		return ErrApplicationClosed
	}

	if err := s.internshipRepo.LoadPublicIssuer(ctx, internship); err != nil {
		return err
	}
	if internship.Issuer.UserID == uuid.Nil {
		return ErrInternshipNotActive
	}

	// Check if student already applied
	existing, err := s.repo.GetByStudentAndInternship(ctx, application.StudentID, application.InternshipID)
	if err == nil && existing != nil {
		return ErrAlreadyApplied
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("check existing application: %w", err)
	}

	if _, err := s.studentRepo.GetResume(ctx, student.ID); errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: upload your PDF CV before applying", ErrInvalidApplicationData)
	} else if err != nil {
		return err
	}

	*application = models.InternshipApplication{InternshipID: application.InternshipID, StudentID: student.ID,
		Status: models.ApplicationStatusSubmitted, AppliedAt: time.Now()}

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

	return s.mapStudentApplications(ctx, apps), total, nil
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

	mapped := s.mapStudentApplications(ctx, []models.InternshipApplication{*application})
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
	document, err := s.studentRepo.GetResume(ctx, application.StudentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return detail, nil
	}
	if err != nil {
		return nil, err
	}
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

	previousStatus := application.Status
	application.Status = newStatus
	if employerNote != nil {
		application.EmployerNote = strings.TrimSpace(*employerNote)
	}

	updatedStatus, err := s.repo.UpdateApplicationStatus(ctx, application, previousStatus)
	if err != nil {
		return nil, err
	}
	if !updatedStatus {
		return nil, ErrInvalidStatusTransition
	}

	// shortcut: delivery is attempted once after saving; use a durable outbox if retries are required.
	if s.sender != nil && application.Student != nil {
		user, err := s.users.GetByIDContext(ctx, application.Student.UserID)
		if err == nil {
			err = s.sender.SendApplicationStatus(ctx, user.Email, internship.Title, application.Status, application.EmployerNote)
		}
		if err != nil && s.log != nil {
			// SMTP errors can contain addresses or message content.
			s.log.Error("application status email failed", "application_id", application.ID)
		}
	}

	updated := mapRecruiterApplication(application)
	return &updated, nil
}

func isValidEmployerStatusTransition(current, next models.InternshipApplicationStatus) bool {
	return current == models.ApplicationStatusSubmitted && (next == models.ApplicationStatusAccepted || next == models.ApplicationStatusRejected)
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

	previousStatus := application.Status
	application.Status = models.ApplicationStatusWithdrawn

	updated, err := s.repo.UpdateApplicationStatus(ctx, application, previousStatus)
	if err != nil {
		return err
	}
	if !updated {
		return ErrInvalidStatusTransition
	}
	return nil
}

func isWithdrawableApplicationStatus(status models.InternshipApplicationStatus) bool {
	return status == models.ApplicationStatusSubmitted
}

func isKnownApplicationStatus(status models.InternshipApplicationStatus) bool {
	switch status {
	case models.ApplicationStatusSubmitted,
		models.ApplicationStatusRejected,
		models.ApplicationStatusAccepted,
		models.ApplicationStatusWithdrawn:
		return true
	default:
		return false
	}
}

func statusesForStudentApplicationFilter(status models.InternshipApplicationStatus) ([]models.InternshipApplicationStatus, error) {
	if status == "" {
		return nil, nil
	}
	if !isKnownApplicationStatus(status) {
		return nil, fmt.Errorf("%w: invalid application status", ErrInvalidApplicationData)
	}
	return []models.InternshipApplicationStatus{status}, nil
}

func (s *InternshipApplicationService) mapStudentApplications(ctx context.Context, applications []models.InternshipApplication) []StudentApplicationSummary {
	mapped := make([]StudentApplicationSummary, len(applications))
	for i := range applications {
		application := &applications[i]
		mapped[i] = StudentApplicationSummary{
			ID:           application.ID,
			InternshipID: application.InternshipID,
			Status:       application.Status,
			EmployerNote: application.EmployerNote,
			AppliedAt:    application.AppliedAt,
			UpdatedAt:    application.UpdatedAt,
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
			issuer := application.Internship.Issuer
			mapped[i].Internship.OrganizationName = issuer.OrganizationName
			mapped[i].Internship.OrganizationLogo = s.profiles.listingLogoURL(ctx, issuer.OrganizationLogo)
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
		ID:           application.ID,
		InternshipID: application.InternshipID,
		StudentID:    application.StudentID,
		Status:       application.Status,
		EmployerNote: application.EmployerNote,
		AppliedAt:    application.AppliedAt,
		CreatedAt:    application.CreatedAt,
		UpdatedAt:    application.UpdatedAt,
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
