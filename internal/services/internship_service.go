package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

var (
	ErrInvalidInternshipData = errors.New("invalid internship data")
	ErrInternshipNotFound    = errors.New("internship not found")
)

type InternshipService struct {
	profiles      *RecruiterProfileService
	repo          *repositories.InternshipRepository
	recruiterRepo *repositories.RecruiterProfileRepository
}

func NewInternshipService(repo *repositories.InternshipRepository, recruiterRepo *repositories.RecruiterProfileRepository, profiles *RecruiterProfileService) *InternshipService {
	return &InternshipService{repo: repo, recruiterRepo: recruiterRepo, profiles: profiles}
}

func (s *InternshipService) CreateInternship(ctx context.Context, internship *models.Internship) error {
	if internship == nil {
		return fmt.Errorf("%w: internship cannot be nil", ErrInvalidInternshipData)
	}
	if internship.IssuedBy == uuid.Nil {
		return fmt.Errorf("%w: issued_by is required", ErrInvalidInternshipData)
	}
	if err := validateInternship(internship); err != nil {
		return err
	}
	_, err := s.recruiterRepo.GetByUserID(ctx, internship.IssuedBy)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: complete your company profile before creating an internship", ErrInvalidInternshipData)
	}
	if err != nil {
		return fmt.Errorf("get recruiter profile: %w", err)
	}
	if internship.Status == "" {
		internship.Status = enums.InternshipStatusPrivate
	}
	if !internship.Status.IsValid() {
		return fmt.Errorf("%w: invalid internship status", ErrInvalidInternshipData)
	}

	if internship.StipendCurrency == "" {
		internship.StipendCurrency = "NPR"
	}
	if internship.VacancyCount <= 0 {
		internship.VacancyCount = 1
	}
	return s.repo.Create(ctx, internship)
}

func (s *InternshipService) UpdateInternship(ctx context.Context, internship *models.Internship) error {
	if internship == nil || internship.ID == uuid.Nil {
		return fmt.Errorf("%w: a valid internship ID is required", ErrInvalidInternshipData)
	}
	if err := validateInternship(internship); err != nil {
		return err
	}

	existing, err := s.GetInternship(ctx, internship.ID)
	if err != nil {
		return err
	}
	_, err = s.recruiterRepo.GetByUserID(ctx, existing.IssuedBy)
	if err != nil {
		return fmt.Errorf("get recruiter profile: %w", err)
	}
	internship.IssuedBy = existing.IssuedBy
	internship.CreatedAt = existing.CreatedAt
	if internship.Status == "" {
		internship.Status = existing.Status
	}
	if !internship.Status.IsValid() {
		return fmt.Errorf("%w: invalid internship status", ErrInvalidInternshipData)
	}
	return s.repo.Update(ctx, internship)
}

func (s *InternshipService) GetInternship(ctx context.Context, id uuid.UUID) (*models.Internship, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid internship ID", ErrInvalidInternshipData)
	}
	internship, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInternshipNotFound
	}
	return internship, err
}

func (s *InternshipService) GetInternshipDetail(ctx context.Context, id uuid.UUID) (*models.Internship, error) {
	internship, err := s.GetInternship(ctx, id)
	if err != nil {
		return nil, err
	}
	if internship.Status != enums.InternshipStatusPublished {
		return nil, ErrInternshipNotFound
	}
	if err := s.repo.LoadPublicIssuer(ctx, internship); err != nil {
		return nil, err
	}
	internship.Issuer.OrganizationLogo = s.profiles.listingLogoURL(ctx, internship.Issuer.OrganizationLogo)
	return internship, nil
}

func (s *InternshipService) DeleteInternship(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: invalid internship ID", ErrInvalidInternshipData)
	}
	if _, err := s.GetInternship(ctx, id); err != nil {
		return err
	}
	return s.repo.DeleteByID(ctx, id)
}

func (s *InternshipService) ListByEmployer(ctx context.Context, employerID uuid.UUID, page, pageSize int) ([]models.Internship, int64, error) {
	if employerID == uuid.Nil {
		return nil, 0, fmt.Errorf("%w: invalid employer ID", ErrInvalidInternshipData)
	}
	return s.repo.ListByEmployer(ctx, employerID, page, pageSize)
}

func (s *InternshipService) SearchInternships(ctx context.Context, filter repositories.InternshipSearchFilter) ([]models.Internship, int64, error) {
	filter.Status = enums.InternshipStatusPublished
	filter.ExcludeExpired = true
	internships, total, err := s.repo.Search(ctx, filter)
	for i := range internships {
		issuer := &internships[i].Issuer
		issuer.OrganizationLogo = s.profiles.listingLogoURL(ctx, issuer.OrganizationLogo)
	}
	return internships, total, err
}

func validateInternship(internship *models.Internship) error {
	internship.Title = strings.TrimSpace(internship.Title)
	internship.Description = strings.TrimSpace(internship.Description)
	if internship.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidInternshipData)
	}
	if len(internship.Description) < 20 {
		return fmt.Errorf("%w: description must be at least 20 characters", ErrInvalidInternshipData)
	}
	if internship.Duration < 0 || internship.StipendAmount < 0 {
		return fmt.Errorf("%w: duration and stipend cannot be negative", ErrInvalidInternshipData)
	}
	if internship.ApplicationDeadline != nil && internship.StartDate != nil && internship.ApplicationDeadline.After(*internship.StartDate) {
		return fmt.Errorf("%w: application deadline must be on or before the start date", ErrInvalidInternshipData)
	}
	return nil
}
