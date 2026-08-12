package repositories

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type StudentRepository struct {
	DB *gorm.DB
}

func NewStudentRepository(db *gorm.DB) *StudentRepository {
	return &StudentRepository{DB: db}
}

func (r *StudentRepository) GetByUserID(userID uuid.UUID) (*models.StudentProfile, error) {
	var p models.StudentProfile
	if err := r.DB.Where("user_id = ?", userID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
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
