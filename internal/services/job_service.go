package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

var (
	ErrInvalidJobData = errors.New("invalid job data")
	ErrJobNotFound    = errors.New("job not found")
)

type JobService struct {
	repo *repositories.JobRepository
}

func NewJobService(repo *repositories.JobRepository) *JobService {
	return &JobService{repo: repo}
}

// CreateJob validates and creates a new job posting.
func (s *JobService) CreateJob(ctx context.Context, j *models.Job) error {
	if j == nil {
		return fmt.Errorf("%w: job cannot be nil", ErrInvalidJobData)
	}
	log.Println("Creating job:", j)

	if j.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidJobData)
	}

	if j.IssuedBy == uuid.Nil {
		return fmt.Errorf("%w: employer_id (issued_by) is required", ErrInvalidJobData)
	}

	if j.SalaryMax < j.SalaryMin {
		return fmt.Errorf("%w: maximum salary cannot be less than minimum salary", ErrInvalidJobData)
	}

	// Set default values if not specified
	if j.Status == "" {
		j.Status = "published"
	}
	if j.SalaryCurrency == "" {
		j.SalaryCurrency = "NPR"
	}
	if j.VacancyCount <= 0 {
		j.VacancyCount = 1
	}
	j.Description = strings.TrimSpace(j.Description)

	if len(j.Description) < 20 {
		return fmt.Errorf("description is too short")
	}

	return s.repo.Create(ctx, j)
}

// UpdateJob validates updates and saves changes to an existing job posting.
func (s *JobService) UpdateJob(ctx context.Context, j *models.Job) error {
	if j == nil || j.ID == uuid.Nil {
		return fmt.Errorf("%w: valid job ID is required for update", ErrInvalidJobData)
	}

	if j.Title == "" {
		return fmt.Errorf("%w: title cannot be empty", ErrInvalidJobData)
	}

	if j.SalaryMax < j.SalaryMin {
		return fmt.Errorf("%w: maximum salary cannot be less than minimum salary", ErrInvalidJobData)
	}

	// Verify job exists before updating
	existing, err := s.repo.GetByID(ctx, j.ID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrJobNotFound, err)
	}

	// Preserve non-updatable fields
	j.IssuedBy = existing.IssuedBy
	j.CreatedAt = existing.CreatedAt

	return s.repo.Update(ctx, j)
}

// GetJob fetches a job posting by its UUID.
func (s *JobService) GetJob(ctx context.Context, id uuid.UUID) (*models.Job, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid job ID", ErrInvalidJobData)
	}
	return s.repo.GetByID(ctx, id)
}

// DeleteJob removes a job posting by ID.
func (s *JobService) DeleteJob(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: invalid job ID", ErrInvalidJobData)
	}
	return s.repo.DeleteByID(ctx, id)
}

// ListByEmployer retrieves paginated jobs posted by a specific employer profile.
func (s *JobService) ListByEmployer(ctx context.Context, employerID uuid.UUID, page, pageSize int) ([]models.Job, int64, error) {
	if employerID == uuid.Nil {
		return nil, 0, fmt.Errorf("%w: invalid employer ID", ErrInvalidJobData)
	}
	return s.repo.ListByEmployer(ctx, employerID, page, pageSize)
}

// ListByCompany retrieves paginated jobs published under a company workspace.
func (s *JobService) ListByCompany(ctx context.Context, companyID uuid.UUID, page, pageSize int) ([]models.Job, int64, error) {
	if companyID == uuid.Nil {
		return nil, 0, fmt.Errorf("%w: invalid company ID", ErrInvalidJobData)
	}
	return s.repo.ListByCompany(ctx, companyID, page, pageSize)
}

// SearchJobs performs multi-criteria filtering with pagination.
func (s *JobService) SearchJobs(ctx context.Context, filter repositories.JobSearchFilter) ([]models.Job, int64, error) {
	return s.repo.Search(ctx, filter)
}
