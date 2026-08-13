package repositories

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type InternshipRepository struct {
	db *gorm.DB
}

func NewInternshipRepository(db *gorm.DB) *InternshipRepository {
	return &InternshipRepository{db: db}
}

// InternshipSearchFilter holds query criteria for internship listings.
type InternshipSearchFilter struct {
	Query          string
	EmployerID     *uuid.UUID // Maps to issued_by column
	Location       string
	WorkMode       string // onsite, remote, hybrid
	InternshipType string // paid, unpaid
	Status         string // draft, published, closed, expired
	IsActive       *bool
	MinStipend     *float64
	ExcludeExpired bool
	Page           int
	PageSize       int
}

// Create inserts a new Internship record.
func (r *InternshipRepository) Create(ctx context.Context, i *models.Internship) error {
	return r.db.WithContext(ctx).Create(i).Error
}

// Update saves all modified fields of an Internship record.
func (r *InternshipRepository) Update(ctx context.Context, i *models.Internship) error {
	return r.db.WithContext(ctx).Save(i).Error
}

// GetByID fetches a single internship by ID.
func (r *InternshipRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.Internship, error) {
	var internship models.Internship
	err := r.db.WithContext(ctx).First(&internship, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &internship, nil
}

// DeleteByID removes an internship record by ID.
func (r *InternshipRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Internship{}).Error
}

// ListByEmployer retrieves all internships created by a specific employer (issued_by) with pagination.
func (r *InternshipRepository) ListByEmployer(ctx context.Context, employerID uuid.UUID, page, pageSize int) ([]models.Internship, int64, error) {
	var internships []models.Internship
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Internship{}).Where("issued_by = ?", employerID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	off, limit := getPagination(page, pageSize)
	err := query.Order("created_at DESC").
		Offset(off).
		Limit(limit).
		Find(&internships).Error

	return internships, total, err
}

// Search executes advanced dynamic filtering and pagination for internships.
func (r *InternshipRepository) Search(ctx context.Context, filter InternshipSearchFilter) ([]models.Internship, int64, error) {
	var internships []models.Internship
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Internship{})

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

	if filter.WorkMode != "" {
		query = query.Where("work_mode = ?", filter.WorkMode)
	}

	if filter.InternshipType != "" {
		query = query.Where("internship_type = ?", filter.InternshipType)
	}

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	if filter.IsActive != nil {
		query = query.Where("is_active = ?", *filter.IsActive)
	}

	if filter.MinStipend != nil {
		query = query.Where("stipend_amount >= ?", *filter.MinStipend)
	}

	if filter.ExcludeExpired {
		query = query.Where("application_deadline IS NULL OR application_deadline >= ?", time.Now())
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	off, limit := getPagination(filter.Page, filter.PageSize)

	err := query.Order("created_at DESC").
		Offset(off).
		Limit(limit).
		Find(&internships).Error

	return internships, total, err
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
