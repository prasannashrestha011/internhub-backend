package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type StudentHandler struct{ Svc *services.StudentService }

func NewStudentHandler(svc *services.StudentService) *StudentHandler {
	return &StudentHandler{Svc: svc}
}

func (h *StudentHandler) GetProfile(c *gin.Context) {
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	profile, err := h.Svc.GetProfile(userID)
	if err != nil {
		writeStudentError(c, err, "failed to fetch profile")
		return
	}
	responses.Success(c, http.StatusOK, "profile fetched", profile)
}

func (h *StudentHandler) UpsertProfile(c *gin.Context) {
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	var profile models.StudentProfile
	if err := c.ShouldBindJSON(&profile); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid profile")
		return
	}
	profile.UserID = userID
	if err := h.Svc.CreateOrUpdateProfile(c.Request.Context(), &profile); err != nil {
		writeStudentError(c, err, "failed to save profile")
		return
	}
	responses.Success(c, http.StatusOK, "profile saved", profile)
}

func (h *StudentHandler) GetApplicationStats(c *gin.Context) {
	profileID, ok := getProfileID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid student profile context")
		return
	}
	stats, err := h.Svc.GetApplicationStats(c.Request.Context(), profileID)
	if err != nil {
		writeStudentError(c, err, "failed to fetch application stats")
		return
	}
	responses.Success(c, http.StatusOK, "application stats", stats)
}

func (h *StudentHandler) UploadProfilePicture(c *gin.Context) {
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (5<<20)+(1<<20))
	file, err := c.FormFile("file")
	if err != nil {
		responses.Error(c, http.StatusBadRequest, services.ErrInvalidProfilePicture.Error())
		return
	}
	profile, err := h.Svc.UploadProfilePicture(c.Request.Context(), userID, file)
	if err != nil {
		writeStudentError(c, err, "failed to upload profile picture")
		return
	}
	responses.Success(c, http.StatusOK, "profile picture saved", profile)
}

func (h *StudentHandler) GetResume(c *gin.Context) {
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	resume, err := h.Svc.GetResume(c.Request.Context(), userID)
	if err != nil {
		writeStudentError(c, err, "failed to fetch CV")
		return
	}
	responses.Success(c, http.StatusOK, "CV", resume)
}

func (h *StudentHandler) UploadResume(c *gin.Context) {
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (10<<20)+(1<<20))
	file, err := c.FormFile("file")
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "upload a PDF CV of at most 10 MiB")
		return
	}
	resume, err := h.Svc.UploadResume(c.Request.Context(), userID, file)
	if err != nil {
		writeStudentError(c, err, "failed to save CV")
		return
	}
	responses.Success(c, http.StatusOK, "CV saved", resume)
}

func (h *StudentHandler) DeleteResume(c *gin.Context) {
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	if err := h.Svc.DeleteResume(c.Request.Context(), userID); err != nil {
		writeStudentError(c, err, "failed to delete CV")
		return
	}
	responses.Success(c, http.StatusOK, "CV deleted", nil)
}

func writeStudentError(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, services.ErrInvalidStudentData), errors.Is(err, services.ErrInvalidProfilePicture):
		responses.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrStudentProfileNotFound):
		responses.Error(c, http.StatusNotFound, "profile not found")
	default:
		responses.Error(c, http.StatusInternalServerError, fallback)
	}
}
