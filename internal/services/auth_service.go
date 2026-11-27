package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

// NOTE: import path spellcheck: make sure it matches module name

type AuthService struct {
	Repo   *repositories.UserRepository
	Cfg    *config.Config
	Logger *logger.Logger
	Sender VerificationSender
}

func NewAuthService(repo *repositories.UserRepository, cfg *config.Config, l *logger.Logger) *AuthService {
	return &AuthService{Repo: repo, Cfg: cfg, Logger: l, Sender: SMTPVerificationSender{Config: cfg.Email}}
}

// HashPassword hashes the plaintext password using bcrypt
func (s *AuthService) HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		s.Logger.Error("bcrypt failed: %v", err)
		return "", err
	}
	return string(b), nil
}

// ComparePassword compares hashed password and plaintext
func (s *AuthService) ComparePassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// GenerateAccessToken creates a signed JWT access token
func (s *AuthService) GenerateAccessToken(user *models.User) (string, error) {
	if !user.CanSignIn() {
		return "", ErrEmailNotVerified
	}
	claims := jwt.MapClaims{
		"sub":  user.ID.String(),
		"role": user.Role,
		"exp":  time.Now().Add(s.Cfg.JWT.AccessExpiry).Unix(),
		"iat":  time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.Cfg.JWT.AccessSecret))
	if err != nil {
		s.Logger.Error("failed to sign access token: %v", err)
		return "", err
	}
	return signed, nil
}

// GenerateRefreshToken creates a secure random refresh token string and persists its hash
func (s *AuthService) GenerateRefreshToken(user *models.User) (string, error) {
	if !user.CanSignIn() {
		return "", ErrEmailNotVerified
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		s.Logger.Error("failed to read random bytes: %v", err)
		return "", err
	}
	token := hex.EncodeToString(b)
	expiresAt := time.Now().Add(s.Cfg.JWT.RefreshExpiry)
	if err := s.Repo.SaveRefreshToken(user.ID, token, expiresAt); err != nil {
		s.Logger.Error("failed to save refresh token: %v", err)
		return "", err
	}
	return token, nil
}

// ValidateRefreshToken verifies the refresh token exists and returns the associated user ID
func (s *AuthService) ValidateRefreshToken(token string) (*models.RefreshToken, error) {
	rt, err := s.Repo.GetRefreshTokenByHash(token)
	if err != nil {
		s.Logger.Debug("refresh token not valid: %v", err)
		return nil, err
	}
	return rt, nil
}

// Consume once and save the replacement together so failures leave the session usable.
func (s *AuthService) RefreshSession(ctx context.Context, token string) (string, string, error) {
	var access, refresh string
	err := s.Repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		auth := *s
		auth.Repo = repositories.NewUserRepository(tx)
		rt, err := auth.ValidateRefreshToken(token)
		if err != nil {
			return err
		}
		user, err := auth.Repo.GetByIDContext(ctx, rt.UserID)
		if err != nil {
			return err
		}
		if !user.CanSignIn() {
			return ErrEmailNotVerified
		}
		if err := auth.RevokeRefreshToken(rt.ID); err != nil {
			return err
		}
		access, err = auth.GenerateAccessToken(user)
		if err != nil {
			return err
		}
		refresh, err = auth.GenerateRefreshToken(user)
		return err
	})
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// RevokeRefreshToken revokes a refresh token by its DB id
func (s *AuthService) RevokeRefreshToken(id uuid.UUID) error {
	return s.Repo.RevokeRefreshTokenByID(id)
}

// RevokeAllForUser revokes all refresh tokens for a user (logout everywhere)
func (s *AuthService) RevokeAllForUser(userID uuid.UUID) error {
	return s.Repo.RevokeAllRefreshTokensForUser(userID)
}
