package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type EmployerProfileHandler struct {
	Svc *services.EmployerProfileService
	Log *logger.Logger
}

func NewEmployerProfileHandler(
	svc *services.EmployerProfileService,
	l *logger.Logger,
) *EmployerProfileHandler {
	return &EmployerProfileHandler{
		Svc: svc,
		Log: l,
	}
}

// GetMyProfile retrieves the employer profile
// for the currently authenticated employer.
func (h *EmployerProfileHandler) GetMyProfile(c *gin.Context) {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(
			c,
			http.StatusUnauthorized,
			err.Error(),
		)
		return
	}

	profile, err := h.Svc.GetByUserID(c.Request.Context(), userID)
	if err != nil {
		h.Log.Error(
			"failed to get employer profile for user %s: %v",
			userID,
			err,
		)

		responses.Error(
			c,
			http.StatusNotFound,
			"employer profile not found",
		)
		return
	}

	responses.Success(
		c,
		http.StatusOK,
		"employer profile retrieved successfully",
		profile,
	)
}

// UpsertMyProfile creates or updates the employer profile
// for the currently authenticated employer.
func (h *EmployerProfileHandler) UpsertMyProfile(c *gin.Context) {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(
			c,
			http.StatusUnauthorized,
			err.Error(),
		)
		return
	}

	var input models.EmployerProfile

	if err := c.ShouldBindJSON(&input); err != nil {
		responses.Error(
			c,
			http.StatusBadRequest,
			"invalid request payload",
		)
		return
	}

	profile, err := h.Svc.CreateOrUpdateProfile(c.Request.Context(), userID, &input)
	if err != nil {
		h.Log.Error(
			"failed to upsert employer profile for user %s: %v",
			userID,
			err,
		)

		responses.Error(
			c,
			http.StatusInternalServerError,
			"failed to save employer profile",
		)
		return
	}

	responses.Success(
		c,
		http.StatusOK,
		"employer profile saved successfully",
		profile,
	)
}

// DeleteMyProfile deletes the employer profile
// associated with the currently authenticated user.
func (h *EmployerProfileHandler) DeleteMyProfile(c *gin.Context) {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(
			c,
			http.StatusUnauthorized,
			err.Error(),
		)
		return
	}

	if err := h.Svc.DeleteByUserID(c.Request.Context(), userID); err != nil {
		h.Log.Error(
			"failed to delete employer profile for user %s: %v",
			userID,
			err,
		)

		responses.Error(
			c,
			http.StatusInternalServerError,
			"failed to delete employer profile",
		)
		return
	}

	responses.Success(
		c,
		http.StatusOK,
		"employer profile deleted successfully",
		nil,
	)
}

func getUserIDFromContext(c *gin.Context) (uuid.UUID, error) {
	uid, ok := c.Get("user_id")
	if !ok {
		return uuid.Nil, errors.New("missing user in context")
	}

	switch value := uid.(type) {

	case string:
		userID, err := uuid.Parse(value)
		if err != nil {
			return uuid.Nil, errors.New("invalid user id in context")
		}

		return userID, nil

	case uuid.UUID:
		return value, nil

	default:
		return uuid.Nil, errors.New("invalid user id in context")
	}
}

func (h *EmployerProfileHandler) UploadOrganizationLogo(c *gin.Context) {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, err.Error())
		return
	}

	file, err := c.FormFile("logo")
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "organization logo is required")
		return
	}

	// Example validation
	if file.Size > 5*1024*1024 {
		responses.Error(c, http.StatusBadRequest, "logo must be smaller than 5MB")
		return
	}

	logoURL, err := h.Svc.UploadOrganizationLogo(c.Request.Context(), userID, file)
	if err != nil {
		h.Log.Error(
			"failed to upload organization logo for user %s: %v",
			userID,
			err,
		)

		responses.Error(
			c,
			http.StatusInternalServerError,
			"failed to upload organization logo",
		)
		return
	}

	responses.Success(
		c,
		http.StatusOK,
		"organization logo uploaded successfully",
		gin.H{
			"organization_logo": logoURL,
		},
	)
}
