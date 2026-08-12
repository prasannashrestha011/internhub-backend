package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.RefreshToken{}); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	userRepo := repositories.NewUserRepository(db)
	cfg := &config.Config{}
	log := logger.New("test")
	authSvc := services.NewAuthService(userRepo, cfg, log)
	authHandler := NewAuthHandler(authSvc, log)

	r := gin.New()
	r.POST("/register/student", authHandler.RegisterStudent)
	r.POST("/login", authHandler.Login)

	return r, db
}

func TestRegisterAndLoginFlow(t *testing.T) {
	r, _ := setupTestRouter(t)

	regBody := map[string]string{
		"email":     "student1@example.com",
		"password":  "Password123!",
		"full_name": "Student One",
	}
	b, _ := json.Marshal(regBody)

	req := httptest.NewRequest(http.MethodPost, "/register/student", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 created, got %d, body: %s", w.Code, w.Body.String())
	}

	// Login
	loginBody := map[string]string{
		"email":    "student1@example.com",
		"password": "Password123!",
	}
	b2, _ := json.Marshal(loginBody)
	req2 := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on login, got %d, body: %s", w2.Code, w2.Body.String())
	}

	// response should contain access_token and refresh_token
	var resp map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal login response: %v", err)
	}
	if !resp["success"].(bool) {
		t.Fatalf("login response success=false: %v", resp)
	}
	data := resp["data"].(map[string]interface{})
	if data["access_token"] == nil || data["refresh_token"] == nil {
		t.Fatalf("tokens not present in response: %v", data)
	}
}
