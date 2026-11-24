package handlers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
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
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	FullName string `json:"full_name" binding:"required,max=255"`
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
	h.register(c, models.RoleStudent)
}

// RegisterEmployer handles employer registration
func (h *AuthHandler) RegisterEmployer(c *gin.Context) {
	h.register(c, models.RoleEmployer)
}

func (h *AuthHandler) register(c *gin.Context, role models.Role) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Error(c, http.StatusBadRequest, "Enter a valid email, full name and password of 8 to 72 characters")
		return
	}
	if strings.TrimSpace(req.FullName) == "" || len(req.Password) > 72 {
		responses.Error(c, http.StatusBadRequest, "Full name is required and password must not exceed 72 bytes")
		return
	}
	user, sent, err := h.AuthSvc.Register(c.Request.Context(), req.Email, req.Password, req.FullName, role)
	if err != nil {
		h.verificationError(c, err)
		return
	}
	message := "Account created. Check your email for a confirmation code."
	if !sent {
		message = "Account created, but we could not send your code. Try resending in 60 seconds."
	}
	responses.Success(c, http.StatusCreated, message, gin.H{
		"user_id": user.ID, "email": user.Email, "verification_required": true,
		"email_sent": sent, "resend_after_seconds": 60,
	})
}

func (h *AuthHandler) VerifyEmail(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email,max=254"`
		Code  string `json:"code" binding:"required,len=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Error(c, http.StatusBadRequest, "Enter your email and six-digit confirmation code")
		return
	}
	if err := h.AuthSvc.VerifyEmail(req.Email, req.Code); err != nil {
		h.verificationError(c, err)
		return
	}
	responses.Success(c, http.StatusOK, "Email confirmed. You can now sign in.", nil)
}

func (h *AuthHandler) ResendVerification(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email,max=254"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Error(c, http.StatusBadRequest, "Enter a valid email address")
		return
	}
	user, err := h.AuthSvc.Repo.GetByEmail(req.Email)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		h.verificationError(c, err)
		return
	}
	if err == nil && !user.CanSignIn() {
		if err := h.AuthSvc.SendVerification(c.Request.Context(), user); err != nil {
			// Keep account existence and delivery/limit state private on this public endpoint.
			if !errors.Is(err, repositories.ErrVerificationCooldown) && !errors.Is(err, repositories.ErrVerificationSendLimit) && !errors.Is(err, repositories.ErrVerificationAttemptLimit) && !errors.Is(err, repositories.ErrAlreadyVerified) && !errors.Is(err, services.ErrEmailUnavailable) {
				h.verificationError(c, err)
				return
			}
		}
	}
	responses.Success(c, http.StatusAccepted, "If this account needs confirmation and its sending limits allow it, we will send a new code. Check your inbox and spam folder. If you have requested five codes, wait an hour before trying again.", gin.H{"resend_after_seconds": 60})
}

func (h *AuthHandler) verificationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrEmailNotVerified):
		c.JSON(http.StatusForbidden, gin.H{"success": false, "code": "EMAIL_NOT_VERIFIED", "message": err.Error()})
	case errors.Is(err, services.ErrEmailExists):
		responses.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, repositories.ErrInvalidVerificationCode):
		responses.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, repositories.ErrVerificationCooldown):
		c.Header("Retry-After", "60")
		responses.Error(c, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, repositories.ErrVerificationSendLimit), errors.Is(err, repositories.ErrVerificationAttemptLimit):
		c.Header("Retry-After", "3600")
		responses.Error(c, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, services.ErrEmailUnavailable):
		responses.Error(c, http.StatusServiceUnavailable, "We could not send a confirmation code. Please try again later.")
	default:
		h.Logger.Error("email verification operation failed")
		responses.Error(c, http.StatusInternalServerError, "Unable to process email confirmation. Please try again.")
	}
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

	if user.Role != models.RoleStudent && user.Role != models.RoleEmployer {
		responses.Error(c, http.StatusForbidden, "account role is no longer supported")
		return
	}

	if err := h.AuthSvc.ComparePassword(user.PasswordHash, req.Password); err != nil {
		responses.Error(c, http.StatusUnauthorized, "Invalid credentials")
		return
	}
	if !user.CanSignIn() {
		h.verificationError(c, services.ErrEmailNotVerified)
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
	if user.Role != models.RoleStudent && user.Role != models.RoleEmployer {
		responses.Error(c, http.StatusForbidden, "account role is no longer supported")
		return
	}
	if !user.CanSignIn() {
		h.verificationError(c, services.ErrEmailNotVerified)
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
	if !user.CanSignIn() {
		h.verificationError(c, services.ErrEmailNotVerified)
		return
	}
	user.PasswordHash = ""
	responses.Success(c, http.StatusOK, "Current user fetched", user)
}
