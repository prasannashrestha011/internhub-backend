package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Role string

const (
	RoleStudent  Role = "student"
	RoleEmployer Role = "employer"
)

type User struct {
	ID              uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	Email           string         `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash    string         `gorm:"not null" json:"-"`
	FullName        string         `gorm:"size:255" json:"full_name"`
	Role            Role           `gorm:"type:text;not null;default:'student'" json:"role"`
	EmailVerifiedAt *time.Time     `json:"email_verified_at"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (u *User) CanSignIn() bool {
	return (u.Role == RoleStudent || u.Role == RoleEmployer) && u.EmailVerifiedAt != nil
}

// BeforeCreate sets UUID if not provided (for sqlite/local dev)
func (u *User) BeforeCreate(tx *gorm.DB) (err error) {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}
