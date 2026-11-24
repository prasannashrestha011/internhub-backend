package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"gorm.io/gorm"
)

var (
	ErrEmailNotVerified = errors.New("confirm your email before signing in")
	ErrEmailExists      = errors.New("an account with this email already exists; sign in to continue or confirm your email")
	ErrEmailUnavailable = errors.New("email delivery is not configured")
)

func (s *AuthService) Register(ctx context.Context, email, password, name string, role models.Role) (*models.User, bool, error) {
	if len(s.Cfg.Email.OTPSecret) < 32 {
		return nil, false, ErrEmailUnavailable
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := s.Repo.GetByEmail(email); err == nil {
		return nil, false, ErrEmailExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	hash, err := s.HashPassword(password)
	if err != nil {
		return nil, false, err
	}
	user := &models.User{Email: email, PasswordHash: hash, FullName: strings.TrimSpace(name), Role: role}
	if err := s.Repo.CreatePendingUser(user); err != nil {
		// A concurrent signup can win the unique email constraint.
		if _, lookupErr := s.Repo.GetByEmail(email); lookupErr == nil {
			return nil, false, ErrEmailExists
		}
		return nil, false, err
	}
	err = s.SendVerification(ctx, user)
	return user, err == nil, nil
}

func (s *AuthService) verificationHash(userID uuid.UUID, code string) string {
	mac := hmac.New(sha256.New, []byte(s.Cfg.Email.OTPSecret))
	_, _ = mac.Write([]byte("email-verification:" + userID.String() + ":" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *AuthService) SendVerification(ctx context.Context, user *models.User) error {
	if len(s.Cfg.Email.OTPSecret) < 32 {
		return ErrEmailUnavailable
	}
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", value.Int64())
	hash := s.verificationHash(user.ID, code)
	if err := s.Repo.ReserveVerificationCode(user.ID, hash, time.Now().UTC()); err != nil {
		return err
	}
	if err := s.Sender.SendVerification(ctx, user.Email, code); err != nil {
		// A failed/ambiguous send must not leave a usable code, or clear a newer resend.
		if clearErr := s.Repo.DB.Model(&models.EmailVerification{}).Where("user_id = ? AND code_hash = ?", user.ID, hash).Update("code_hash", "").Error; clearErr != nil {
			s.Logger.Error("failed to invalidate email verification for user %s", user.ID)
		}
		// SMTP errors can contain addresses or message content; do not log them.
		s.Logger.Error("verification email delivery failed for user %s", user.ID)
		return ErrEmailUnavailable
	}
	return nil
}

func (s *AuthService) VerifyEmail(email, code string) error {
	if len(s.Cfg.Email.OTPSecret) < 32 {
		return ErrEmailUnavailable
	}
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
		return repositories.ErrInvalidVerificationCode
	}
	user, err := s.Repo.GetByEmail(email)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repositories.ErrInvalidVerificationCode
	}
	if err != nil {
		return err
	}
	return s.Repo.VerifyEmailCode(user.ID, s.verificationHash(user.ID, code), time.Now().UTC())
}
