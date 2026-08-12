package repositories

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type InterviewRepository struct {
	DB *gorm.DB
}

func NewInterviewRepository(db *gorm.DB) *InterviewRepository {
	return &InterviewRepository{DB: db}
}

func (r *InterviewRepository) Create(i *models.Interview) error {
	return r.DB.Create(i).Error
}

func (r *InterviewRepository) GetByID(id uuid.UUID) (*models.Interview, error) {
	var out models.Interview
	if err := r.DB.First(&out, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *InterviewRepository) ListByJob(jobID uuid.UUID) ([]models.Interview, error) {
	var out []models.Interview
	if err := r.DB.Where("job_id = ?", jobID).Order("scheduled_at desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *InterviewRepository) ListByStudent(studentID uuid.UUID) ([]models.Interview, error) {
	var out []models.Interview
	if err := r.DB.Where("student_id = ?", studentID).Order("scheduled_at desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *InterviewRepository) ListByEmployer(employerID uuid.UUID) ([]models.Interview, error) {
	var out []models.Interview
	if err := r.DB.Where("employer_id = ?", employerID).Order("scheduled_at desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *InterviewRepository) Update(i *models.Interview) error {
	return r.DB.Save(i).Error
}

func (r *InterviewRepository) DeleteByID(id uuid.UUID) error {
	return r.DB.Where("id = ?", id).Delete(&models.Interview{}).Error
}
