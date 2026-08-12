package repositories

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

// ApplicationStatusHistoryRepository handles persistence for ApplicationStatusHistory
type ApplicationStatusHistoryRepository struct {
	DB *gorm.DB
}

// NewApplicationStatusHistoryRepository creates a new repository
func NewApplicationStatusHistoryRepository(db *gorm.DB) *ApplicationStatusHistoryRepository {
	return &ApplicationStatusHistoryRepository{DB: db}
}

// Create persists a history record
func (r *ApplicationStatusHistoryRepository) Create(h *models.ApplicationStatusHistory) error {
	return r.DB.Create(h).Error
}

// ListByApplication returns history records for an application ordered by newest first
func (r *ApplicationStatusHistoryRepository) ListByApplication(applicationID uuid.UUID) ([]models.ApplicationStatusHistory, error) {
	var out []models.ApplicationStatusHistory
	if err := r.DB.Where("application_id = ?", applicationID).Order("created_at desc").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
