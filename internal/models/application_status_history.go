package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ApplicationStatusHistory tracks transitions for applications
type ApplicationStatusHistory struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	ApplicationID uuid.UUID `gorm:"type:uuid;index;not null" json:"application_id"`
	FromStatus    string    `gorm:"size:64" json:"from_status"`
	ToStatus      string    `gorm:"size:64" json:"to_status"`
	ChangedBy     string    `gorm:"size:128" json:"changed_by"` // user id (uuid string)
	Reason        string    `gorm:"type:text" json:"reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (h *ApplicationStatusHistory) BeforeCreate(tx *gorm.DB) (err error) {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	return nil
}
