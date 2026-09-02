package repositories

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type InternshipApplicationRepository struct {
	db *gorm.DB
}

// RecruiterApplicationFilter scopes application results to one employer and
// optionally narrows them by candidate name, internship, or status.
type RecruiterApplicationFilter struct {
	EmployerID   uuid.UUID
	Query        string
	InternshipID *uuid.UUID
	Status       models.InternshipApplicationStatus
	Page         int
	PageSize     int
}

// StudentApplicationFilter scopes application results to one student and can
// optionally match one or more workflow statuses.
type StudentApplicationFilter struct {
	StudentID uuid.UUID
	Statuses  []models.InternshipApplicationStatus
	Page      int
	PageSize  int
}

func NewInternshipApplicationRepository(db *gorm.DB) *InternshipApplicationRepository {
	return &InternshipApplicationRepository{db: db}
}

func (r *InternshipApplicationRepository) Create(ctx context.Context, a *models.InternshipApplication) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *InternshipApplicationRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.InternshipApplication, error) {
	var out models.InternshipApplication
	if err := r.db.WithContext(ctx).
		Preload("Internship").
		Preload("Student").
		Preload("Student.Documents", "is_default = ?", true).
		First(&out, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *InternshipApplicationRepository) GetByStudentAndInternship(ctx context.Context, studentID, internshipID uuid.UUID) (*models.InternshipApplication, error) {
	var out models.InternshipApplication
	result := r.db.WithContext(ctx).
		Where("student_id = ? AND internship_id = ?", studentID, internshipID).
		Limit(1).
		Find(&out)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &out, nil
}

func (r *InternshipApplicationRepository) ListByStudent(
	ctx context.Context,
	filter StudentApplicationFilter,
) ([]models.InternshipApplication, int64, error) {
	var out []models.InternshipApplication
	var total int64

	query := r.db.WithContext(ctx).
		Model(&models.InternshipApplication{}).
		Where("student_id = ?", filter.StudentID)

	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	off, limit := getPagination(filter.Page, filter.PageSize)
	err := query.Preload("Internship").
		Preload("Internship.Issuer").
		Order("applied_at DESC").
		Offset(off).
		Limit(limit).
		Find(&out).Error

	return out, total, err
}

func (r *InternshipApplicationRepository) ListByInternship(ctx context.Context, internshipID uuid.UUID, page, pageSize int) ([]models.InternshipApplication, int64, error) {
	var out []models.InternshipApplication
	var total int64

	query := r.db.WithContext(ctx).
		Model(&models.InternshipApplication{}).
		Where("internship_id = ?", internshipID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	off, limit := getPagination(page, pageSize)
	err := query.Preload("Student").
		Preload("Student.Documents", "is_default = ?", true).
		Order("applied_at DESC").
		Offset(off).
		Limit(limit).
		Find(&out).Error

	return out, total, err
}

// ListForEmployer returns only applications for internships issued by the
// supplied employer. Ownership is enforced inside the database query so an
// application from another employer can never enter the result set.
func (r *InternshipApplicationRepository) ListForEmployer(
	ctx context.Context,
	filter RecruiterApplicationFilter,
) ([]models.InternshipApplication, int64, error) {
	var out []models.InternshipApplication
	var total int64

	query := r.db.WithContext(ctx).
		Model(&models.InternshipApplication{}).
		Joins("JOIN internships ON internships.id = internship_applications.internship_id").
		Joins("JOIN student_profiles ON student_profiles.id = internship_applications.student_id").
		Where("internships.issued_by = ?", filter.EmployerID)

	if queryText := strings.TrimSpace(filter.Query); queryText != "" {
		query = query.Where(
			"LOWER(student_profiles.full_name) LIKE ?",
			"%"+strings.ToLower(queryText)+"%",
		)
	}
	if filter.InternshipID != nil {
		query = query.Where("internship_applications.internship_id = ?", *filter.InternshipID)
	}
	if filter.Status != "" {
		query = query.Where("internship_applications.status = ?", filter.Status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset, limit := getPagination(filter.Page, filter.PageSize)
	err := query.
		Preload("Student").
		Preload("Internship").
		Order("internship_applications.applied_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&out).Error

	return out, total, err
}

func (r *InternshipApplicationRepository) Update(ctx context.Context, a *models.InternshipApplication) error {
	return r.db.WithContext(ctx).Save(a).Error
}
