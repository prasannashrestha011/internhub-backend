package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RecruiterProfile struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"user_id"`

	OrganizationName string `gorm:"size:200;not null" json:"organization_name"`
	Designation      string `gorm:"size:100" json:"designation,omitempty"`

	OrganizationLogo    string `gorm:"type:text" json:"organization_logo,omitempty"`
	OrganizationWebsite string `gorm:"size:255" json:"organization_website,omitempty"`
	OrganizationAddress string `gorm:"size:255" json:"organization_address,omitempty"`
	Industry            string `gorm:"size:150" json:"industry,omitempty"`
	OrganizationSize    string `gorm:"size:50" json:"organization_size,omitempty"`
	OrganizationAbout   string `gorm:"type:text" json:"organization_about,omitempty"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ep *RecruiterProfile) BeforeCreate(tx *gorm.DB) error {
	if ep.ID == uuid.Nil {
		ep.ID = uuid.New()
	}
	return nil
}
