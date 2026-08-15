package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type StudentHandler struct {
	Svc  *services.StudentService
	Repo *repositories.StudentRepository
	Cfg  *config.Config
	Log  *logger.Logger
}

func NewStudentHandler(svc *services.StudentService, repo *repositories.StudentRepository, cfg *config.Config, l *logger.Logger) *StudentHandler {
	return &StudentHandler{Svc: svc, Repo: repo, Cfg: cfg, Log: l}
}

// GetProfile returns the current student's profile (or 404)
func (h *StudentHandler) GetProfile(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	p, err := h.Svc.GetProfile(userID)
	if err != nil {
		writeStudentError(c, err, "failed to fetch profile")
		return
	}
	responses.Success(c, http.StatusOK, "profile fetched", p)
}

// UpsertProfile creates or updates the student's profile
func (h *StudentHandler) UpsertProfile(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	var payload models.StudentProfile
	if err := c.ShouldBindJSON(&payload); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}
	payload.UserID = userID
	payload.UpdatedAt = time.Now()
	if err := h.Svc.CreateOrUpdateProfile(&payload); err != nil {
		writeStudentError(c, err, "failed to save profile")
		return
	}
	responses.Success(c, http.StatusOK, "profile saved", payload)
}

// UploadDocument handles multipart file upload for student documents
func (h *StudentHandler) UploadDocument(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	// ensure profile exists
	p, err := h.Svc.GetProfile(userID)
	if err != nil {
		writeStudentError(c, err, "failed to fetch profile")
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "missing file")
		return
	}
	// Upload to minio via service then persist metadata
	objectKey, size, mimeType, err := h.Svc.UploadDocument(userID, p.ID, file)
	if err != nil {
		writeStudentError(c, err, "failed to upload document")
		return
	}
	doc := &models.StudentDocument{
		ProfileID: p.ID,
		UserID:    userID,
		ObjectKey: objectKey,
		FileName:  file.Filename,
		MimeType:  mimeType,
		Size:      size,
		IsDefault: false,
	}
	if err := h.Svc.AddDocument(doc); err != nil {
		writeStudentError(c, err, "failed to save document metadata")
		return
	}
	responses.Success(c, http.StatusCreated, "uploaded", doc)
}

// ListDocuments returns documents for the student's profile
func (h *StudentHandler) ListDocuments(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	docs, err := h.Svc.ListDocuments(userID)
	if err != nil {
		writeStudentError(c, err, "failed to list documents")
		return
	}
	responses.Success(c, http.StatusOK, "documents", docs)
}

// SetDefaultDocument sets a document as default
func (h *StudentHandler) SetDefaultDocument(c *gin.Context) {
	docIDStr := c.Param("id")
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid document id")
		return
	}
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	if err := h.Svc.SetDefaultDocument(userID, docID); err != nil {
		writeStudentError(c, err, "failed to set default document")
		return
	}
	responses.Success(c, http.StatusOK, "default set", nil)
}

// DeleteDocument deletes a student's document
func (h *StudentHandler) DeleteDocument(c *gin.Context) {
	docIDStr := c.Param("id")
	docID, err := uuid.Parse(docIDStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid document id")
		return
	}
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	if err := h.Svc.DeleteDocumentByUserID(userID, docID); err != nil {
		writeStudentError(c, err, "failed to delete document")
		return
	}
	responses.Success(c, http.StatusOK, "deleted", nil)
}

func writeStudentError(c *gin.Context, err error, fallback string) {
	switch {
	case errors.Is(err, services.ErrInvalidStudentData):
		responses.Error(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrStudentProfileNotFound):
		responses.Error(c, http.StatusNotFound, "profile not found")
	case errors.Is(err, services.ErrStudentDocumentNotFound):
		responses.Error(c, http.StatusNotFound, "document not found")
	default:
		responses.Error(c, http.StatusInternalServerError, fallback)
	}
}
