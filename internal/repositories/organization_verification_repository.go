package repositories

import (
	"context"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OrganizationVerificationRepository struct {
	DB *gorm.DB
}

func NewOrganizationVerificationRepository(
	db *gorm.DB,
) *OrganizationVerificationRepository {
	return &OrganizationVerificationRepository{
		DB: db,
	}
}

type VerificationSearchFilter struct {
	Status   enums.OrganizationVerificationStatus
	Page     int
	PageSize int
}

// recruiter submits/updates verification request
func (r *OrganizationVerificationRepository) Upsert(
	ctx context.Context,
	verification *models.OrganizationVerification,
) error {
	return r.DB.
		WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "recruiter_profile_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"method",
				"organization_email",
				"email_domain",
				"document_type",
				"document_object_key",
				"status",
				"submitted_at",
				"reviewed_by",
				"rejection_reason",
				"review_notes",
				"reviewed_at",
				"verified_at",
				"updated_at",
			}),
		}).
		Create(verification).
		Error
}

// recruiter views own verification
func (r *OrganizationVerificationRepository) GetByRecruiterProfileID(
	ctx context.Context,
	recruiterProfileID uuid.UUID,
) (*models.OrganizationVerification, error) {

	var verification models.OrganizationVerification

	err := r.DB.Preload("RecruiterProfile").
		WithContext(ctx).
		Where(
			"recruiter_profile_id = ?",
			recruiterProfileID,
		).
		First(&verification).
		Error

	if err != nil {
		return nil, err
	}

	return &verification, nil
}

// GetByOrganizationEmail returns a verification request submitted with the given email.
func (r *OrganizationVerificationRepository) GetByOrganizationEmail(
	ctx context.Context,
	organizationEmail string,
) (*models.OrganizationVerification, error) {
	var verification models.OrganizationVerification

	err := r.DB.
		WithContext(ctx).
		Where("LOWER(organization_email) = ?", organizationEmail).
		First(&verification).
		Error
	if err != nil {
		return nil, err
	}

	return &verification, nil
}

// admin views one verification
func (r *OrganizationVerificationRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*models.OrganizationVerification, error) {

	var verification models.OrganizationVerification

	err := r.DB.
		WithContext(ctx).
		Preload("RecruiterProfile").
		Preload("RecruiterProfile.User").
		First(&verification, "id = ?", id).
		Error

	if err != nil {
		return nil, err
	}

	return &verification, nil
}

// update MinIO document object key
func (r *OrganizationVerificationRepository) UpdateDocument(
	ctx context.Context,
	id uuid.UUID,
	documentType string,
	objectKey string,
) error {

	return r.DB.
		WithContext(ctx).
		Model(&models.OrganizationVerification{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"document_type":       documentType,
			"document_object_key": objectKey,
		}).
		Error
}

func (r *OrganizationVerificationRepository) UpdateReview(
	ctx context.Context,
	id uuid.UUID,
	updates map[string]interface{},
) error {

	return r.DB.
		WithContext(ctx).
		Model(&models.OrganizationVerification{}).
		Where("id = ?", id).
		Updates(updates).
		Error
}

// admin sees pending requests
func (r *OrganizationVerificationRepository) GetPending(
	ctx context.Context,
	limit int,
	offset int,
) ([]models.OrganizationVerification, error) {

	var verifications []models.OrganizationVerification

	err := r.DB.
		WithContext(ctx).
		Preload("RecruiterProfile").
		Preload("RecruiterProfile.User").
		Where(
			"status = ?",
			enums.OrganizationVerificationPending,
		).
		Order("submitted_at ASC").
		Limit(limit).
		Offset(offset).
		Find(&verifications).
		Error

	if err != nil {
		return nil, err
	}

	return verifications, nil
}

func (r *OrganizationVerificationRepository) CountPending(
	ctx context.Context,
) (int64, error) {

	var count int64

	err := r.DB.
		WithContext(ctx).
		Model(&models.OrganizationVerification{}).
		Where(
			"status = ?",
			enums.OrganizationVerificationPending,
		).
		Count(&count).
		Error

	return count, err
}

func (r *OrganizationVerificationRepository) CountByStatus(
	ctx context.Context,
	status enums.OrganizationVerificationStatus,
) (int64, error) {
	var count int64

	query := r.DB.WithContext(ctx).Model(&models.OrganizationVerification{})
	if status != "" {
		query = query.Where("status = ?", status)
	}

	err := query.Count(&count).Error
	return count, err
}

// admin filtering
func (r *OrganizationVerificationRepository) GetByStatus(
	ctx context.Context,
	status enums.OrganizationVerificationStatus,
	limit int,
	offset int,
) ([]models.OrganizationVerification, error) {
	var verifications []models.OrganizationVerification
	query := r.DB.
		WithContext(ctx).
		Preload("RecruiterProfile").
		Preload("RecruiterProfile.User").
		Order("created_at DESC").
		Limit(limit).
		Offset(offset)

	switch status {
	case "":
		// no filter — all statuses
	case enums.OrganizationVerificationReviewed:
		query = query.Where(
			"status IN (?, ?)",
			enums.OrganizationVerificationApproved,
			enums.OrganizationVerificationRejected,
		)
	default:
		query = query.Where("status = ?", status)
	}

	if err := query.Find(&verifications).Error; err != nil {
		return nil, err
	}
	return verifications, nil
}

func (r *OrganizationVerificationRepository) ExistsByRecruiterProfileID(

	ctx context.Context,
	recruiterProfileID uuid.UUID,
) (bool, error) {

	var count int64

	err := r.DB.
		WithContext(ctx).
		Model(&models.OrganizationVerification{}).
		Where(
			"recruiter_profile_id = ?",
			recruiterProfileID,
		).
		Count(&count).
		Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}
