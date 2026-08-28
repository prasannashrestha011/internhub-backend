package repositories

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type StudentRepository struct {
	DB *gorm.DB
}

// StudentApplicationStats is the dashboard-facing summary of a student's
// internship applications. Active applications are all non-final applications
// (submitted, reviewing, or shortlisted), while pending applications are those
// that have only been submitted.
type StudentApplicationStats struct {
	TotalApplications       int64 `json:"total_applications"`
	ActiveApplications      int64 `json:"active_applications"`
	ApprovedApplications    int64 `json:"approved_applications"`
	RejectedApplications    int64 `json:"rejected_applications"`
	PendingApplications     int64 `json:"pending_applications"`
	UnderReviewApplications int64 `json:"under_review_applications"`
	ShortlistedApplications int64 `json:"shortlisted_applications"`
	WithdrawnApplications   int64 `json:"withdrawn_applications"`
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
	var p models.StudentProfile
	if err := r.DB.Where("user_id = ?", userID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *StudentRepository) GetApplicationStats(
	ctx context.Context,
	profileID uuid.UUID,
) (*StudentApplicationStats, error) {
	var stats StudentApplicationStats

	err := r.DB.
		WithContext(ctx).
		Model(&models.InternshipApplication{}).
		Where("student_id = ?", profileID).
		Select(`
			COUNT(*) AS total_applications,
			COALESCE(SUM(CASE WHEN status IN (?, ?, ?) THEN 1 ELSE 0 END), 0) AS active_applications,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS approved_applications,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS rejected_applications,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS pending_applications,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS under_review_applications,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS shortlisted_applications,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS withdrawn_applications
		`,
			models.ApplicationStatusSubmitted,
			models.ApplicationStatusReviewing,
			models.ApplicationStatusShortlisted,
			models.ApplicationStatusAccepted,
			models.ApplicationStatusRejected,
			models.ApplicationStatusSubmitted,
			models.ApplicationStatusReviewing,
			models.ApplicationStatusShortlisted,
			models.ApplicationStatusWithdrawn,
		).
		Scan(&stats).
		Error
	if err != nil {
		return nil, err
	}

	return &stats, nil
}

func (r *StudentRepository) CreateOrUpdateProfile(p *models.StudentProfile) error {
	// Upsert by user_id
	var existing models.StudentProfile
	if err := r.DB.Where("user_id = ?", p.UserID).First(&existing).Error; err == nil {
		p.ID = existing.ID
		p.CreatedAt = existing.CreatedAt
		return r.DB.Save(p).Error
	}
	return r.DB.Create(p).Error
}

func (r *StudentRepository) AddEducation(e *models.StudentEducation) error {
	return r.DB.Create(e).Error
}

func (r *StudentRepository) ListEducations(profileID uuid.UUID) ([]models.StudentEducation, error) {
	var out []models.StudentEducation
	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *StudentRepository) AddSkill(s *models.StudentSkill) error {
	return r.DB.Create(s).Error
}

func (r *StudentRepository) ListSkills(profileID uuid.UUID) ([]models.StudentSkill, error) {
	var out []models.StudentSkill
	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *StudentRepository) AddProject(pj *models.StudentProject) error {
	return r.DB.Create(pj).Error
}

func (r *StudentRepository) ListProjects(profileID uuid.UUID) ([]models.StudentProject, error) {
	var out []models.StudentProject
	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *StudentRepository) AddCertification(c *models.StudentCertification) error {
	return r.DB.Create(c).Error
}

func (r *StudentRepository) ListCertifications(profileID uuid.UUID) ([]models.StudentCertification, error) {
	var out []models.StudentCertification
	if err := r.DB.Where("profile_id = ?", profileID).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *StudentRepository) AddDocument(d *models.StudentDocument) error {
	return r.DB.Create(d).Error
}

func (r *StudentRepository) ListDocuments(profileID uuid.UUID) ([]models.StudentDocument, error) {
	var out []models.StudentDocument
	if err := r.DB.Where("profile_id = ?", profileID).Order("is_default desc, created_at desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *StudentRepository) SetDefaultDocument(profileID, docID uuid.UUID) error {
	// clear existing defaults
	if err := r.DB.Model(&models.StudentDocument{}).Where("profile_id = ?", profileID).Update("is_default", false).Error; err != nil {
		return err
	}
	return r.DB.Model(&models.StudentDocument{}).Where("id = ? AND profile_id = ?", docID, profileID).Update("is_default", true).Error
}

func (r *StudentRepository) DeleteDocumentByID(docID uuid.UUID) error {
	return r.DB.Where("id = ?", docID).Delete(&models.StudentDocument{}).Error
}

func (r *StudentRepository) GetDocumentByID(docID uuid.UUID) (*models.StudentDocument, error) {
	var d models.StudentDocument
	if err := r.DB.First(&d, "id = ?", docID).Error; err != nil {
		return nil, err
	}
	return &d, nil
}
