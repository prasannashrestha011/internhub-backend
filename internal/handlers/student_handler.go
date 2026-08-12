package handlers

import (
	"log"
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
	p, err := h.Repo.GetByUserID(userID)
	log.Println("GetProfile: fetched profile:", p, "error:", err)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "profile not found")
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
	if err := h.Repo.CreateOrUpdateProfile(&payload); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to save profile")
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
	p, err := h.Repo.GetByUserID(userID)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "profile not found")
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
		h.Log.Error("upload failed: %v", err)
		responses.Error(c, http.StatusInternalServerError, "failed to upload")
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
	if err := h.Repo.AddDocument(doc); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to save document metadata")
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
	p, err := h.Repo.GetByUserID(userID)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "profile not found")
		return
	}
	docs, err := h.Repo.ListDocuments(p.ID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list documents")
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
	// ensure profile exists
	p, err := h.Repo.GetByUserID(userID)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "profile not found")
		return
	}
	if err := h.Repo.SetDefaultDocument(p.ID, docID); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to set default")
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
	// ensure ownership
	doc, err := h.Repo.GetDocumentByID(docID)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "document not found")
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
	if doc.UserID != userID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	// delete object from storage
	if err := h.Svc.DeleteDocument(doc.ObjectKey); err != nil {
		// log and continue; we still attempt to remove DB record to keep state consistent
		h.Log.Error("minio delete failed: %v", err)
	}
	if err := h.Repo.DeleteDocumentByID(doc.ID); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to delete document record")
		return
	}
	responses.Success(c, http.StatusOK, "deleted", nil)
}
