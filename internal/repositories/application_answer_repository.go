package repositories

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type ApplicationAnswerRepository struct {
	DB *gorm.DB
}

func NewApplicationAnswerRepository(db *gorm.DB) *ApplicationAnswerRepository {
	return &ApplicationAnswerRepository{DB: db}
}

func (r *ApplicationAnswerRepository) Create(a *models.ApplicationAnswer) error {
	return r.DB.Create(a).Error
}

func (r *ApplicationAnswerRepository) ListByApplication(appID uuid.UUID) ([]models.ApplicationAnswer, error) {
	var out []models.ApplicationAnswer
	if err := r.DB.Where("application_id = ?", appID).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
