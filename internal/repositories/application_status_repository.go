package repositories

import "gorm.io/gorm"

// ApplicationStatusRepository is an alias for ApplicationStatusHistoryRepository kept for
// backward compatibility. Use NewApplicationStatusHistoryRepository to construct a new instance.

type ApplicationStatusRepository = ApplicationStatusHistoryRepository

func NewApplicationStatusRepository(db *gorm.DB) *ApplicationStatusRepository {
	return NewApplicationStatusHistoryRepository(db)
}
