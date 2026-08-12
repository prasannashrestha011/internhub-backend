package repositories

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type EmployerProfileRepository struct {
	DB *gorm.DB
}

func NewEmployerProfileRepository(db *gorm.DB) *EmployerProfileRepository {
	return &EmployerProfileRepository{
		DB: db,
	}
}

// Upsert creates a new employer profile or updates the existing
// organization/profile information when user_id already exists.
func (r *EmployerProfileRepository) Upsert(ctx context.Context, profile *models.EmployerProfile) error {
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

// GetByUserID retrieves an employer profile by user_id.
// User information is also preloaded.
func (r *EmployerProfileRepository) GetByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*models.EmployerProfile, error) {
	var profile models.EmployerProfile

	err := r.DB.
		Preload("User").
		Where("user_id = ?", userID).
		First(&profile).
		Error
	if err != nil {
		return nil, err
	}

	return &profile, nil
}

// GetByID retrieves an employer profile by profile ID.
func (r *EmployerProfileRepository) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*models.EmployerProfile, error) {
	var profile models.EmployerProfile

	err := r.DB.
		Preload("User").
		First(&profile, "id = ?", id).
		Error
	if err != nil {
		return nil, err
	}

	return &profile, nil
}

func (r *EmployerProfileRepository) UpdateEmployeeVerification(ctx context.Context, userID uuid.UUID, isVerified bool) error {
	return r.DB.Model(&models.EmployerProfile{}).
		Where("user_id = ?", userID).
		Update("verification_status", isVerified).
		Error
}

// UpdateVerificationStatus keeps the profile-level status in sync with the
// associated organization verification review.
func (r *EmployerProfileRepository) UpdateVerificationStatus(
	ctx context.Context,
	employerProfileID uuid.UUID,
	status string,
) error {
	return r.DB.
		WithContext(ctx).
		Model(&models.EmployerProfile{}).
		Where("id = ?", employerProfileID).
		Update("verification_status", status).
		Error
}

// DeleteByUserID removes an employer profile associated with the given user.
func (r *EmployerProfileRepository) DeleteByUserID(
	ctx context.Context,
	userID uuid.UUID,
) error {
	return r.DB.
		Where("user_id = ?", userID).
		Delete(&models.EmployerProfile{}).
		Error
}

func (r *EmployerProfileRepository) UpdateOrganizationLogo(
	ctx context.Context,
	userID uuid.UUID,
	objectKey string,
) error {
	return r.DB.
		WithContext(ctx).
		Model(&models.EmployerProfile{}).
		Where("user_id = ?", userID).
		Update("organization_logo", objectKey).
		Error
}
