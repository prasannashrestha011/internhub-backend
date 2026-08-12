package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type AuthHandler struct {
	AuthSvc *services.AuthService
	Logger  *logger.Logger
}

func NewAuthHandler(svc *services.AuthService, l *logger.Logger) *AuthHandler {
	return &AuthHandler{AuthSvc: svc, Logger: l}
}

// DTOs
type registerRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	FullName string `json:"full_name" binding:"required"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type tokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// RegisterStudent handles student registration
func (h *AuthHandler) RegisterStudent(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.ErrorWithDetails(c, http.StatusBadRequest, "Invalid input", gin.H{"error": err.Error()})
		return
	}

	// Hash password
	hash, err := h.AuthSvc.HashPassword(req.Password)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to process password")
		return
	}

	user := &models.User{
		Email:        req.Email,
		PasswordHash: hash,
		FullName:     req.FullName,
		Role:         models.RoleStudent,
	}

	if err := h.AuthSvc.Repo.Create(user); err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to create user")
		return
	}

	responses.Success(c, http.StatusCreated, "Student registered successfully", gin.H{"user_id": user.ID})
}

// RegisterEmployer handles employer registration
func (h *AuthHandler) RegisterEmployer(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.ErrorWithDetails(c, http.StatusBadRequest, "Invalid input", gin.H{"error": err.Error()})
		return
	}

	hash, err := h.AuthSvc.HashPassword(req.Password)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to process password")
		return
	}

	user := &models.User{
		Email:        req.Email,
		PasswordHash: hash,
		FullName:     req.FullName,
		Role:         models.RoleEmployer,
	}

	if err := h.AuthSvc.Repo.Create(user); err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to create user")
		return
	}

	responses.Success(c, http.StatusCreated, "Employer registered successfully", gin.H{"user_id": user.ID})
}

// Login authenticates user and returns tokens
// Login authenticates user and sets access and refresh tokens in HTTP-Only cookies
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.ErrorWithDetails(c, http.StatusBadRequest, "Invalid input", gin.H{"error": err.Error()})
		return
	}

	user, err := h.AuthSvc.Repo.GetByEmail(req.Email)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	if err := h.AuthSvc.ComparePassword(user.PasswordHash, req.Password); err != nil {
		responses.Error(c, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	access, err := h.AuthSvc.GenerateAccessToken(user)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to generate access token")
		return
	}

	refresh, err := h.AuthSvc.GenerateRefreshToken(user)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to generate refresh token")
		return
	}

	// 2. Set SameSite policy (Lax works for same-origin or localhost dev)
	c.SetSameSite(http.SameSiteLaxMode)

	// 3. Set Access Token Cookie
	accessMaxAge := int(h.AuthSvc.Cfg.JWT.AccessExpiry.Seconds())
	c.SetCookie(
		"access_token", // Cookie name
		access,         // Cookie value (JWT)
		accessMaxAge,   // MaxAge in seconds
		"/",            // Path
		"",             // Domain (leave empty for current host)
		true,           // Secure (true forces HTTPS only)
		true,           // HttpOnly (prevents client-side JS/XSS access)
	)

	// 4. Set Refresh Token Cookie
	refreshMaxAge := int(h.AuthSvc.Cfg.JWT.RefreshExpiry.Seconds())
	c.SetCookie(
		"refresh_token",
		refresh,
		refreshMaxAge,
		"/", // Or scope to "/api/v1/auth/refresh"
		"",
		true,
		true,
	)

	// 5. Return sanitized user details (excluding raw tokens)
	responses.Success(c, http.StatusOK, "Logged in successfully", gin.H{
		"user": gin.H{
			"id":    user.ID,
			"email": user.Email,
		},
	})
}

// Refresh issues new access and refresh tokens given a valid refresh token
func (h *AuthHandler) Refresh(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		responses.ErrorWithDetails(c, http.StatusBadRequest, "Invalid input", gin.H{"error": err.Error()})
		return
	}

	rt, err := h.AuthSvc.ValidateRefreshToken(body.RefreshToken)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	user, err := h.AuthSvc.Repo.GetByID(rt.UserID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "User not found")
		return
	}

	// Revoke used refresh token
	if err := h.AuthSvc.RevokeRefreshToken(rt.ID); err != nil {
		// log but continue
		h.Logger.Warn("failed to revoke refresh token: %v", err)
	}

	access, err := h.AuthSvc.GenerateAccessToken(user)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to generate access token")
		return
	}

	newRefresh, err := h.AuthSvc.GenerateRefreshToken(user)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to generate refresh token")
		return
	}

	expiresAt := time.Now().Add(h.AuthSvc.Cfg.JWT.AccessExpiry)
	responses.Success(c, http.StatusOK, "Token refreshed", tokenResponse{AccessToken: access, RefreshToken: newRefresh, Expiry: expiresAt})
}

// Logout revokes provided refresh token
func (h *AuthHandler) Logout(c *gin.Context) {
	// 1. Read the refresh token from the HTTP cookie
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil || refreshToken == "" {
		responses.Error(c, http.StatusBadRequest, "Refresh token missing from cookie")
		return
	}

	// 2. Validate token
	rt, err := h.AuthSvc.ValidateRefreshToken(refreshToken)
	if err != nil {
		// Clear stale/invalid cookie on client
		h.clearRefreshTokenCookie(c)
		responses.Error(c, http.StatusBadRequest, "Invalid refresh token")
		return
	}

	// 3. Revoke token in backend storage
	if err := h.AuthSvc.RevokeRefreshToken(rt.ID); err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to revoke token")
		return
	}

	// 4. Clear the cookie by setting maxAge = -1
	h.clearRefreshTokenCookie(c)

	responses.Success(c, http.StatusOK, "Logged out successfully", nil)
}

// Helper to clear the refresh token cookie
func (h *AuthHandler) clearRefreshTokenCookie(c *gin.Context) {
	// SetCookie(name, value, maxAge, path, domain, secure, httpOnly)
	// maxAge -1 deletes the cookie immediately on the browser
	c.SetCookie("access_token", "", -1, "/", "", true, true)
	c.SetCookie("refresh_token", "", -1, "/", "", true, true)
}

// Me returns current authenticated user
func (h *AuthHandler) Me(c *gin.Context) {
	uidv, exists := c.Get("user_id")
	if !exists {
		responses.Error(c, http.StatusUnauthorized, "Not authenticated")
		return
	}
	uidStr, ok := uidv.(string)
	if !ok {
		responses.Error(c, http.StatusInternalServerError, "Invalid user id format")
		return
	}
	uid, err := uuid.Parse(uidStr)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Invalid user id")
		return
	}

	user, err := h.AuthSvc.Repo.GetByID(uid)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "User not found")
		return
	}

	// Hide sensitive fields
	user.PasswordHash = ""
	responses.Success(c, http.StatusOK, "Current user fetched", user)
}
