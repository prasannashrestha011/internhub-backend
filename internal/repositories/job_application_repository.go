package repositories

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type JobApplicationRepository struct {
	DB *gorm.DB
}

func NewJobApplicationRepository(db *gorm.DB) *JobApplicationRepository {
	return &JobApplicationRepository{DB: db}
}

func (r *JobApplicationRepository) Create(a *models.JobApplication) error {
	return r.DB.Create(a).Error
}

func (r *JobApplicationRepository) GetByID(id uuid.UUID) (*models.JobApplication, error) {
	var out models.JobApplication
	if err := r.DB.First(&out, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *JobApplicationRepository) ListByStudent(studentID uuid.UUID) ([]models.JobApplication, error) {
	var out []models.JobApplication
	if err := r.DB.Where("student_id = ?", studentID).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *JobApplicationRepository) ListByJob(jobID uuid.UUID) ([]models.JobApplication, error) {
	var out []models.JobApplication
	if err := r.DB.Where("job_id = ?", jobID).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *JobApplicationRepository) Update(a *models.JobApplication) error {
	return r.DB.Save(a).Error
}
