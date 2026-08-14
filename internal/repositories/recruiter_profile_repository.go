package repositories

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type RecruiterProfileRepository struct {
	DB *gorm.DB
}

func NewRecruiterProfileRepository(db *gorm.DB) *RecruiterProfileRepository {
	return &RecruiterProfileRepository{
		DB: db,
	}
}

// Upsert creates a new recruiter profile or updates the existing
// organization/profile information when user_id already exists.
func (r *RecruiterProfileRepository) Upsert(ctx context.Context, profile *models.RecruiterProfile) error {
	return r.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"organization_name",
			"designation",
			"organization_logo",
			"organization_website",
			"organization_address",
			"organization_about",
			"industry",
			"organization_size",
			"updated_at",
		}),
	}).Create(profile).Error
}

// GetByUserID retrieves an recruiter profile by user_id.
// User information is also preloaded.
func (r *RecruiterProfileRepository) GetByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*models.RecruiterProfile, error) {
	var profile models.RecruiterProfile

	err := r.DB.
		Preload("User").
		Preload("Verification").
		Where("user_id = ?", userID).
		First(&profile).
		Error
	if err != nil {
		return nil, err
	}

	return &profile, nil
}

// GetByID retrieves an recruiter profile by profile ID.
func (r *RecruiterProfileRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*models.RecruiterProfile, error) {
	var profile models.RecruiterProfile

	err := r.DB.
		Preload("User").
		First(&profile, "id = ?", id).
		Error
	if err != nil {
		return nil, err
	}

	return &profile, nil
}

func (r *RecruiterProfileRepository) UpdateEmployeeVerification(ctx context.Context, userID uuid.UUID, isVerified bool) error {
	return r.DB.Model(&models.RecruiterProfile{}).
		Where("user_id = ?", userID).
		Update("verification_status", isVerified).
		Error
}

// UpdateVerificationStatus keeps the profile-level status in sync with the
// associated organization verification review.
func (r *RecruiterProfileRepository) UpdateVerificationStatus(
	ctx context.Context,
	recruiterProfileID uuid.UUID,
	status string,
) error {
	return r.DB.
		WithContext(ctx).
		Model(&models.RecruiterProfile{}).
		Where("id = ?", recruiterProfileID).
		Update("verification_status", status).
		Error
}

// DeleteByUserID removes an recruiter profile associated with the given user.
func (r *RecruiterProfileRepository) DeleteByUserID(
	ctx context.Context,
	userID uuid.UUID,
) error {
	return r.DB.
		Where("user_id = ?", userID).
		Delete(&models.RecruiterProfile{}).
		Error
}

func (r *RecruiterProfileRepository) UpdateOrganizationLogo(
	ctx context.Context,
	userID uuid.UUID,
	objectKey string,
) error {
	return r.DB.
		WithContext(ctx).
		Model(&models.RecruiterProfile{}).
		Where("user_id = ?", userID).
		Update("organization_logo", objectKey).
		Error
}
