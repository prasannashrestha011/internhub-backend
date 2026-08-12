package repositories

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type JobRepository struct {
	db *gorm.DB
}

func NewJobRepository(db *gorm.DB) *JobRepository {
	return &JobRepository{db: db}
}

// JobSearchFilter holds query criteria for job listings.
type JobSearchFilter struct {
	Query           string
	EmployerID      *uuid.UUID // Maps to issued_by column
	Location        string
	Remote          *bool
	JobType         string // full_time, part_time, internship, contract
	WorkMode        string // onsite, remote, hybrid
	ExperienceLevel string // entry, junior, mid
	Status          string // draft, published, closed, expired
	IsActive        *bool
	MinSalary       *float64
	ExcludeExpired  bool // Filter out jobs past application_deadline
	Page            int
	PageSize        int
}

// Create inserts a new Job record.
func (r *JobRepository) Create(ctx context.Context, j *models.Job) error {
	return r.db.WithContext(ctx).Create(j).Error
}

// Update saves all modified fields of a Job record.
func (r *JobRepository) Update(ctx context.Context, j *models.Job) error {
	return r.db.WithContext(ctx).Save(j).Error
}

// GetByID fetches a single Job by ID along with its associated Company details.
func (r *JobRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Job, error) {
	var job models.Job
	err := r.db.WithContext(ctx).
		Preload("Company").
		First(&job, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// DeleteByID removes a job record by ID.
func (r *JobRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Job{}).Error
}

// ListByEmployer retrieves all jobs created by a specific employer (issued_by) with pagination.
func (r *JobRepository) ListByEmployer(ctx context.Context, employerID uuid.UUID, page, pageSize int) ([]models.Job, int64, error) {
	var jobs []models.Job
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Job{}).Where("issued_by = ?", employerID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	off, limit := getPagination(page, pageSize)
	err := query.Preload("Company").
		Order("created_at DESC").
		Offset(off).
		Limit(limit).
		Find(&jobs).Error

	return jobs, total, err
}

// ListByCompany retrieves all jobs posted under a specific company workspace.
func (r *JobRepository) ListByCompany(ctx context.Context, companyID uuid.UUID, page, pageSize int) ([]models.Job, int64, error) {
	var jobs []models.Job
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Job{}).Where("company_id = ?", companyID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	off, limit := getPagination(page, pageSize)
	err := query.
		Order("created_at DESC").
		Offset(off).
		Limit(limit).
		Find(&jobs).Error

	return jobs, total, err
}

// Search executes advanced dynamic filtering and pagination for jobs.
func (r *JobRepository) Search(ctx context.Context, filter JobSearchFilter) ([]models.Job, int64, error) {
	var jobs []models.Job
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Job{})

	if filter.Query != "" {
		like := "%" + filter.Query + "%"
		query = query.Where("title ILIKE ? OR description ILIKE ? OR required_skills ILIKE ?", like, like, like)
	}

	if filter.EmployerID != nil {
		query = query.Where("issued_by = ?", *filter.EmployerID)
	}

	if filter.Location != "" {
		query = query.Where("location ILIKE ?", "%"+filter.Location+"%")
	}

	if filter.Remote != nil {
		query = query.Where("remote = ?", *filter.Remote)
	}

	if filter.JobType != "" {
		query = query.Where("job_type = ?", filter.JobType)
	}

	if filter.WorkMode != "" {
		query = query.Where("work_mode = ?", filter.WorkMode)
	}

	if filter.ExperienceLevel != "" {
		query = query.Where("experience_level = ?", filter.ExperienceLevel)
	}

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if filter.IsActive != nil {
		query = query.Where("is_active = ?", *filter.IsActive)
	}

	if filter.MinSalary != nil {
		query = query.Where("salary_max >= ?", *filter.MinSalary)
	}

	if filter.ExcludeExpired {
		query = query.Where("application_deadline IS NULL OR application_deadline >= ?", time.Now())
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	off, limit := getPagination(filter.Page, filter.PageSize)

	err := query.Preload("Company").
		Order("created_at DESC").
		Offset(off).
		Limit(limit).
		Find(&jobs).Error

	return jobs, total, err
}

// Helper to sanitize pagination parameters
func getPagination(page, pageSize int) (offset int, limit int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 10
	}
	return (page - 1) * pageSize, pageSize
}
