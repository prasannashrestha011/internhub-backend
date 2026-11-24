package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
	"os"
)

type capturedVerification struct {
	mu    sync.Mutex
	code  string
	fail  bool
	sends int
}

func (s *capturedVerification) SendVerification(_ context.Context, _, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.code, s.sends = code, s.sends+1
	if s.fail {
		return errors.New("SMTP unavailable")
	}
	return nil
}

func verificationTestService(t *testing.T, dialector gorm.Dialector) (*AuthService, *capturedVerification) {
	t.Helper()
	db, err := gorm.Open(dialector, &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	if db.Dialector.Name() == "sqlite" {
		sqlDB.SetMaxOpenConns(1)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{JWT: config.JWTConfig{AccessSecret: "test-jwt", AccessExpiry: time.Hour, RefreshExpiry: time.Hour}, Email: config.EmailConfig{OTPSecret: strings.Repeat("test-otp-", 4)}}
	svc := NewAuthService(repositories.NewUserRepository(db), cfg, logger.New("test"))
	sender := &capturedVerification{}
	svc.Sender = sender
	return svc, sender
}

func sqliteVerificationService(t *testing.T) (*AuthService, *capturedVerification) {
	return verificationTestService(t, sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared&_foreign_keys=on"))
}

func TestAccountEmailVerification(t *testing.T) {
	for _, role := range []models.Role{models.RoleStudent, models.RoleEmployer} {
		t.Run(string(role), func(t *testing.T) {
			svc, sender := sqliteVerificationService(t)
			user, sent, err := svc.Register(context.Background(), "New.User@EXAMPLE.COM", "Password123!", " New User ", role)
			if err != nil || !sent || user.Email != "new.user@example.com" || user.FullName != "New User" || user.EmailVerifiedAt != nil {
				t.Fatalf("registration: user=%+v sent=%v err=%v", user, sent, err)
			}
			if len(sender.code) != 6 || strings.Trim(sender.code, "0123456789") != "" {
				t.Fatal("code must have exactly six digits")
			}
			var verification models.EmailVerification
			if err := svc.Repo.DB.First(&verification, "user_id = ?", user.ID).Error; err != nil {
				t.Fatal(err)
			}
			if verification.CodeHash == sender.code || len(verification.CodeHash) != 64 {
				t.Fatal("code must only be stored as HMAC")
			}
			if _, err := svc.GenerateAccessToken(user); !errors.Is(err, ErrEmailNotVerified) {
				t.Fatalf("pending access token: %v", err)
			}
			if _, err := svc.GenerateRefreshToken(user); !errors.Is(err, ErrEmailNotVerified) {
				t.Fatalf("pending refresh token: %v", err)
			}
			if _, _, err := svc.Register(context.Background(), user.Email, "Different123!", "Changed", models.RoleEmployer); !errors.Is(err, ErrEmailExists) {
				t.Fatalf("duplicate registration: %v", err)
			}
			unchanged, _ := svc.Repo.GetByEmail(user.Email)
			if unchanged.Role != role || unchanged.FullName != "New User" || svc.ComparePassword(unchanged.PasswordHash, "Password123!") != nil {
				t.Fatal("duplicate changed existing account")
			}
			if err := svc.VerifyEmail("NEW.USER@example.com", sender.code); err != nil {
				t.Fatal(err)
			}
			confirmed, _ := svc.Repo.GetByID(user.ID)
			if confirmed.EmailVerifiedAt == nil {
				t.Fatal("account not confirmed")
			}
			if _, err := svc.GenerateAccessToken(confirmed); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.GenerateRefreshToken(confirmed); err != nil {
				t.Fatal(err)
			}
			if err := svc.VerifyEmail(user.Email, sender.code); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
				t.Fatalf("code replay: %v", err)
			}
		})
	}
}

func TestVerificationReplacementAndLimits(t *testing.T) {
	svc, sender := sqliteVerificationService(t)
	user, _, err := svc.Register(context.Background(), "limits@example.com", "Password123!", "User", models.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := svc.SendVerification(context.Background(), user); !errors.Is(err, repositories.ErrVerificationCooldown) {
		t.Fatalf("cooldown: %v", err)
	}
	// A deterministic leading-zero code exercises the string contract.
	if err := svc.Repo.ReserveVerificationCode(user.ID, svc.verificationHash(user.ID, "000123"), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	wrong := "111111"
	if err := svc.VerifyEmail(user.Email, wrong); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := svc.VerifyEmail(user.Email, "000123"); err != nil {
		t.Fatalf("leading zero: %v", err)
	}

	user, _, err = svc.Register(context.Background(), "attempts@example.com", "Password123!", "User", models.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		err := svc.Repo.VerifyEmailCode(user.ID, "wrong-hash", now.Add(time.Duration(i)*time.Second))
		want := repositories.ErrInvalidVerificationCode
		if i == 4 {
			want = repositories.ErrVerificationAttemptLimit
		}
		if !errors.Is(err, want) {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	if err := svc.VerifyEmail(user.Email, sender.code); !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
		t.Fatalf("correct code after lockout: %v", err)
	}
	if err := svc.Repo.ReserveVerificationCode(user.ID, "replacement", now.Add(2*time.Minute)); !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
		t.Fatalf("resend bypassed attempts: %v", err)
	}
	if err := svc.Repo.ReserveVerificationCode(user.ID, "replacement", now.Add(time.Hour+time.Minute)); err != nil {
		t.Fatalf("limit did not expire: %v", err)
	}

	user, _, err = svc.Register(context.Background(), "sends@example.com", "Password123!", "User", models.RoleEmployer)
	if err != nil {
		t.Fatal(err)
	}
	oldCode := sender.code
	for i := 1; i < 5; i++ {
		if err := svc.Repo.ReserveVerificationCode(user.ID, svc.verificationHash(user.ID, "000456"), now.Add(time.Duration(i+1)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.VerifyEmail(user.Email, oldCode); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
		t.Fatalf("replaced code: %v", err)
	}
	if err := svc.Repo.ReserveVerificationCode(user.ID, "sixth", now.Add(6*time.Minute)); !errors.Is(err, repositories.ErrVerificationSendLimit) {
		t.Fatalf("send cap: %v", err)
	}
	if err := svc.Repo.VerifyEmailCode(user.ID, svc.verificationHash(user.ID, "000456"), now.Add(15*time.Minute)); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
		t.Fatalf("expired code: %v", err)
	}
}

func TestFailedDeliveryIsRecoverable(t *testing.T) {
	svc, sender := sqliteVerificationService(t)
	sender.fail = true
	user, sent, err := svc.Register(context.Background(), "failure@example.com", "Password123!", "User", models.RoleStudent)
	if err != nil || sent || user == nil {
		t.Fatalf("registration must remain recoverable: %v", err)
	}
	if err := svc.VerifyEmail(user.Email, sender.code); !errors.Is(err, repositories.ErrInvalidVerificationCode) {
		t.Fatalf("failed delivery code remains usable: %v", err)
	}
	sender.fail = false
	past := time.Now().UTC().Add(-2 * time.Minute)
	if err := svc.Repo.DB.Model(&models.EmailVerification{}).Where("user_id = ?", user.ID).Update("last_sent_at", past).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.SendVerification(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyEmail(user.Email, sender.code); err != nil {
		t.Fatal(err)
	}
}

func testConcurrentVerification(t *testing.T, svc *AuthService, sender *capturedVerification) {
	t.Helper()
	user, _, err := svc.Register(context.Background(), uuid.NewString()+"@example.com", "Password123!", "User", models.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		go func() { results <- svc.VerifyEmail(user.Email, sender.code) }()
	}
	successes := 0
	for i := 0; i < cap(results); i++ {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, repositories.ErrInvalidVerificationCode) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("code used %d times", successes)
	}
	user, _, err = svc.Register(context.Background(), uuid.NewString()+"@example.com", "Password123!", "User", models.RoleStudent)
	if err != nil {
		t.Fatal(err)
	}
	code := sender.code
	for i := 0; i < cap(results); i++ {
		go func() { results <- svc.Repo.VerifyEmailCode(user.ID, "incorrect", time.Now().UTC()) }()
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; !errors.Is(err, repositories.ErrInvalidVerificationCode) && !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
			t.Fatal(err)
		}
	}
	if err := svc.VerifyEmail(user.Email, code); !errors.Is(err, repositories.ErrVerificationAttemptLimit) {
		t.Fatalf("parallel attempts bypassed limit: %v", err)
	}
	var verification models.EmailVerification
	if err := svc.Repo.DB.First(&verification, "user_id = ?", user.ID).Error; err != nil || verification.FailedAttempts != 5 {
		t.Fatalf("attempt count=%d: %v", verification.FailedAttempts, err)
	}

	user, _, err = svc.Register(context.Background(), uuid.NewString()+"@example.com", "Password123!", "User", models.RoleEmployer)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(results); i++ {
		go func() { results <- svc.SendVerification(context.Background(), user) }()
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; !errors.Is(err, repositories.ErrVerificationCooldown) {
			t.Fatalf("parallel resend: %v", err)
		}
	}
	verification = models.EmailVerification{}
	if err := svc.Repo.DB.First(&verification, "user_id = ?", user.ID).Error; err != nil || verification.SendCount != 1 {
		t.Fatal(err)
	}
}

func TestConcurrentVerification(t *testing.T) {
	svc, sender := sqliteVerificationService(t)
	testConcurrentVerification(t, svc, sender)
}

func TestPostgresConcurrentVerification(t *testing.T) {
	dsn := os.Getenv("OTP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set OTP_TEST_POSTGRES_DSN to a disposable database to test PostgreSQL row locks")
	}
	svc, sender := verificationTestService(t, postgres.Open(dsn))
	testConcurrentVerification(t, svc, sender)
	// Canonical uniqueness must also be enforced for direct/concurrent writes.
	first := &models.User{Email: uuid.NewString() + "@example.com", PasswordHash: "hash", Role: models.RoleStudent}
	if err := svc.Repo.Create(first); err != nil {
		t.Fatal(err)
	}
	duplicate := &models.User{Email: strings.ToUpper(first.Email), PasswordHash: "hash", Role: models.RoleEmployer}
	if err := svc.Repo.Create(duplicate); err == nil {
		t.Fatal("case-insensitive duplicate accepted")
	}
}
