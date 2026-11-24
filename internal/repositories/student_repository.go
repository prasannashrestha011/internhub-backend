package repositories

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type StudentRepository struct {
	DB *gorm.DB
}

type StudentApplicationStats struct {
	TotalApplications     int64 `json:"total_applications"`
	PendingApplications   int64 `json:"pending_applications"`
	ApprovedApplications  int64 `json:"approved_applications"`
	RejectedApplications  int64 `json:"rejected_applications"`
	WithdrawnApplications int64 `json:"withdrawn_applications"`
}

func NewStudentRepository(db *gorm.DB) *StudentRepository {
	return &StudentRepository{DB: db}
}

func (r *StudentRepository) ResolveProfileID(
	userID uuid.UUID,
) (uuid.UUID, error) {
	var profile models.StudentProfile

	if err := r.DB.
		Where("user_id = ?", userID).
		First(&profile).Error; err != nil {
		return uuid.Nil, err
	}

	return profile.ID, nil
}
func (r *StudentRepository) GetByUserID(userID uuid.UUID) (*models.StudentProfile, error) {
	return r.GetByUserIDContext(context.Background(), userID)
}

func (r *StudentRepository) GetByUserIDContext(ctx context.Context, userID uuid.UUID) (*models.StudentProfile, error) {
	var p models.StudentProfile
	if err := r.DB.WithContext(ctx).Where("user_id = ?", userID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *StudentRepository) GetApplicationStats(
	ctx context.Context,
	profileID uuid.UUID,
) (*StudentApplicationStats, error) {
	var stats StudentApplicationStats

	err := r.DB.WithContext(ctx).Model(&models.InternshipApplication{}).Where("student_id = ?", profileID).
		Select(`COUNT(*) AS total_applications,
 COALESCE(SUM(CASE WHEN status = 'submitted' THEN 1 ELSE 0 END), 0) AS pending_applications,
 COALESCE(SUM(CASE WHEN status = 'accepted' THEN 1 ELSE 0 END), 0) AS approved_applications,
 COALESCE(SUM(CASE WHEN status = 'rejected' THEN 1 ELSE 0 END), 0) AS rejected_applications,
 COALESCE(SUM(CASE WHEN status = 'withdrawn' THEN 1 ELSE 0 END), 0) AS withdrawn_applications`).Scan(&stats).Error
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

func (r *StudentRepository) CreateOrUpdateProfile(ctx context.Context, p *models.StudentProfile) error {
	// Upsert by user_id
	db := r.DB.WithContext(ctx)
	var existing models.StudentProfile
	err := db.Where("user_id = ?", p.UserID).First(&existing).Error
	if err == nil {
		p.ID = existing.ID
		p.CreatedAt = existing.CreatedAt
		p.ProfilePictureKey = existing.ProfilePictureKey
		result := db.Model(p).
			Omit(clause.Associations, "ProfilePictureKey").Select("*").Updates(p)
		if result.Error == nil && result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return result.Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Omit(clause.Associations).Create(p).Error
}

func (r *StudentRepository) UpdateProfilePicture(ctx context.Context, userID uuid.UUID, objectKey string) (string, error) {
	var oldKey string
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile models.StudentProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&profile).Error; err != nil {
			return err
		}
		oldKey = profile.ProfilePictureKey
		return tx.Model(&profile).Update("profile_picture_key", objectKey).Error
	})
	return oldKey, err
}

func (r *StudentRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.StudentProfile, error) {
	var student models.StudentProfile
	err := r.DB.WithContext(ctx).First(&student, "id = ?", id).Error
	return &student, err
}

// GetResume selects the latest CV; legacy document rows stay in the database.
func (r *StudentRepository) GetResume(ctx context.Context, profileID uuid.UUID) (*models.StudentDocument, error) {
	var resume models.StudentDocument
	err := r.DB.WithContext(ctx).Where("profile_id = ?", profileID).Order("created_at DESC, id DESC").First(&resume).Error
	return &resume, err
}

func (r *StudentRepository) SaveResume(ctx context.Context, resume *models.StudentDocument) (string, error) {
	var oldKey string
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile models.StudentProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&profile, "id = ? AND user_id = ?", resume.ProfileID, resume.UserID).Error; err != nil {
			return err
		}
		existing, err := NewStudentRepository(tx).GetResume(ctx, profile.ID)
		if err == nil {
			resume.ID, resume.CreatedAt, oldKey = existing.ID, existing.CreatedAt, existing.ObjectKey
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Save(resume).Error
	})
	return oldKey, err
}

func (r *StudentRepository) DeleteResume(ctx context.Context, profileID, userID uuid.UUID) ([]models.StudentDocument, error) {
	var documents []models.StudentDocument
	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var profile models.StudentProfile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&profile, "id = ? AND user_id = ?", profileID, userID).Error; err != nil {
			return err
		}
		if err := tx.Where("profile_id = ?", profileID).Find(&documents).Error; err != nil {
			return err
		}
		// Remove legacy CVs too so an older file cannot become the current CV.
		return tx.Where("profile_id = ?", profileID).Delete(&models.StudentDocument{}).Error
	})
	return documents, err
}
