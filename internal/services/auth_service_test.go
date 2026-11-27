package services

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func testRefreshRotation(t *testing.T, svc *AuthService) {
	t.Helper()
	now := time.Now().UTC()
	user := &models.User{Email: uuid.NewString() + "@example.com", PasswordHash: "hash", Role: models.RoleStudent, EmailVerifiedAt: &now}
	if err := svc.Repo.Create(user); err != nil {
		t.Fatal(err)
	}
	token, err := svc.GenerateRefreshToken(user)
	if err != nil {
		t.Fatal(err)
	}
	// A failed replacement must roll back consumption of the old token.
	if err := svc.Repo.DB.Callback().Create().Before("gorm:create").Register("test:reject_refresh", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "RefreshToken" {
			tx.AddError(errors.New("replacement insert failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.RefreshSession(context.Background(), token); err == nil {
		t.Fatal("refresh succeeded despite storage failure")
	}
	if err := svc.Repo.DB.Callback().Create().Remove("test:reject_refresh"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ValidateRefreshToken(token); err != nil {
		t.Fatalf("failed rotation consumed original token: %v", err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, replacement, err := svc.RefreshSession(context.Background(), token)
			if err == nil {
				_, err = svc.ValidateRefreshToken(replacement)
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("one token produced %d successful refreshes", successes)
	}
}

func TestRefreshRotation(t *testing.T) {
	svc, _ := sqliteVerificationService(t)
	testRefreshRotation(t, svc)
}

func TestPostgresRefreshRotation(t *testing.T) {
	dsn := os.Getenv("OTP_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set OTP_TEST_POSTGRES_DSN to a disposable PostgreSQL database")
	}
	svc, _ := verificationTestService(t, postgres.Open(dsn))
	testRefreshRotation(t, svc)
}
