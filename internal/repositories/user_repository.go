package repositories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
)

type UserRepository struct {
	DB *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{DB: db}
}

func (r *UserRepository) Create(user *models.User) error {
	return r.DB.Create(user).Error
}

func (r *UserRepository) GetByEmail(email string) (*models.User, error) {
	var u models.User
	if err := r.DB.Where("LOWER(TRIM(email)) = ?", strings.ToLower(strings.TrimSpace(email))).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepository) GetByID(id uuid.UUID) (*models.User, error) {
	return r.GetByIDContext(context.Background(), id)
}

func (r *UserRepository) GetByIDContext(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var u models.User
	if err := r.DB.WithContext(ctx).First(&u, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// Refresh token helpers
func (r *UserRepository) SaveRefreshToken(userID uuid.UUID, token string, expiry time.Time) error {
	h := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(h[:])
	rt := &models.RefreshToken{
		UserID:    userID,
		TokenHash: hash,
		ExpiresAt: expiry,
	}
	return r.DB.Create(rt).Error
}

func (r *UserRepository) GetRefreshTokenByHash(token string) (*models.RefreshToken, error) {
	h := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(h[:])
	var rt models.RefreshToken
	if err := r.DB.Where("token_hash = ? AND revoked = false AND expires_at > ?", hash, time.Now()).First(&rt).Error; err != nil {
		return nil, err
	}
	return &rt, nil
}

func (r *UserRepository) RevokeRefreshTokenByID(id uuid.UUID) error {
	result := r.DB.Model(&models.RefreshToken{}).
		Where("id = ? AND revoked = false AND expires_at > ?", id, time.Now()).Update("revoked", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *UserRepository) RevokeAllRefreshTokensForUser(userID uuid.UUID) error {
	return r.DB.Model(&models.RefreshToken{}).Where("user_id = ?", userID).Update("revoked", true).Error
}
