package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/services"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

type testVerificationSender struct{ code string }

func (s *testVerificationSender) SendVerification(_ context.Context, _, code string) error {
	s.code = code
	return nil
}

func TestRegistrationConfirmationAndLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: gormlog.Default.LogMode(gormlog.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{JWT: config.JWTConfig{AccessSecret: "test-secret", AccessExpiry: time.Hour, RefreshExpiry: time.Hour}, Email: config.EmailConfig{OTPSecret: strings.Repeat("otp-test", 4)}}
	log := logger.New("test")
	svc := services.NewAuthService(repositories.NewUserRepository(db), cfg, log)
	sender := &testVerificationSender{}
	svc.Sender = sender
	handler := NewAuthHandler(svc, log)
	router := gin.New()
	router.POST("/register/student", handler.RegisterStudent)
	router.POST("/register/employer", handler.RegisterEmployer)
	router.POST("/verify-email", handler.VerifyEmail)
	router.POST("/resend-verification", handler.ResendVerification)
	router.POST("/login", handler.Login)
	router.POST("/refresh", handler.Refresh)
	request := func(path string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s: got %d want %d: %s", path, response.Code, want, response.Body)
		}
		return response
	}
	for _, role := range []string{"student", "employer"} {
		email := role + "@example.com"
		credentials := gin.H{"email": email, "password": "Password123!"}
		response := request("/register/"+role, gin.H{"email": email, "password": "Password123!", "full_name": "New User"}, 201)
		if len(response.Result().Cookies()) != 0 || !strings.Contains(response.Body.String(), `"verification_required":true`) {
			t.Fatal("registration should require verification without cookies")
		}
		response = request("/login", credentials, 403)
		if len(response.Result().Cookies()) != 0 || !strings.Contains(response.Body.String(), "EMAIL_NOT_VERIFIED") {
			t.Fatal("unverified login issued cookies or lost error code")
		}
		request("/login", gin.H{"email": email, "password": "wrong-password"}, 401)
		user, _ := svc.Repo.GetByEmail(email)
		// A stale/externally created refresh token must not bypass verification.
		if err := svc.Repo.SaveRefreshToken(user.ID, "test-refresh-"+role, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		request("/refresh", gin.H{"refresh_token": "test-refresh-" + role}, 403)
		request("/verify-email", gin.H{"email": email, "code": "12e345"}, 400)
		request("/verify-email", gin.H{"email": email, "code": sender.code}, 200)
		response = request("/login", credentials, 200)
		if len(response.Result().Cookies()) != 2 {
			t.Fatal("verified login did not set session cookies")
		}
		request("/verify-email", gin.H{"email": email, "code": sender.code}, 400)
		// Email confirmation must not create an employer profile.
		var count int64
		if err := db.Model(&models.RecruiterProfile{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("confirmation created an employer profile: %v", err)
		}
	}
	// Retired admin accounts cannot log in, renew old tokens, or obtain new ones.
	student, err := svc.Repo.GetByEmail("student@example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacy := &models.User{Email: "legacy-admin@example.com", PasswordHash: student.PasswordHash, Role: "admin", EmailVerifiedAt: &now}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	request("/login", gin.H{"email": legacy.Email, "password": "Password123!"}, 403)
	if err := svc.Repo.SaveRefreshToken(legacy.ID, "legacy-admin-refresh", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	request("/refresh", gin.H{"refresh_token": "legacy-admin-refresh"}, 403)
	if _, err := svc.GenerateAccessToken(legacy); err == nil {
		t.Fatal("retired admin obtained an access token")
	}
	if _, err := svc.GenerateRefreshToken(legacy); err == nil {
		t.Fatal("retired admin obtained a refresh token")
	}
	unknown := request("/resend-verification", gin.H{"email": "unknown@example.com"}, 202).Body.String()
	verified := request("/resend-verification", gin.H{"email": "student@example.com"}, 202).Body.String()
	if unknown != verified {
		t.Fatal("resend response reveals existing accounts")
	}
	request("/register/student", gin.H{"email": "pending@example.com", "password": "Password123!", "full_name": "New User"}, 201)
	cooldown := request("/resend-verification", gin.H{"email": "pending@example.com"}, 202).Body.String()
	if cooldown != unknown {
		t.Fatal("resend limits reveal pending accounts")
	}
	request("/register/employer", gin.H{"email": "PENDING@example.com", "password": "Different123!", "full_name": "Changed"}, 409)
	request("/register/student", gin.H{"email": "empty@example.com", "password": "Password123!", "full_name": "   "}, 400)
}
