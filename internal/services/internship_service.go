package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

var (
	ErrInvalidInternshipData = errors.New("invalid internship data")
	ErrInternshipNotFound    = errors.New("internship not found")
)

type InternshipService struct {
	repo *repositories.InternshipRepository
}

func NewInternshipService(repo *repositories.InternshipRepository) *InternshipService {
	return &InternshipService{repo: repo}
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
	if internship.Status == "" {
		internship.Status = "published"
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
	internship.IssuedBy = existing.IssuedBy
	internship.CreatedAt = existing.CreatedAt
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
	return s.repo.Search(ctx, filter)
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
