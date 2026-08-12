package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type OrganizationVerificationHandler struct {
	Svc *services.OrganizationVerificationService
	Log *logger.Logger
}

func NewOrganizationVerificationHandler(svc *services.OrganizationVerificationService, l *logger.Logger) *OrganizationVerificationHandler {
	return &OrganizationVerificationHandler{Svc: svc, Log: l}
}

type submitOrganizationVerificationInput struct {
	OrganizationEmail string `json:"organization_email"`
}
type reviewOrganizationVerificationInput struct {
	Status          enums.OrganizationVerificationStatus `json:"status"`
	RejectionReason string                               `json:"rejection_reason"`
	ReviewNotes     string                               `json:"review_notes"`
}

func (h *OrganizationVerificationHandler) SubmitVerification(c *gin.Context) {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	var input submitOrganizationVerificationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid request payload")
		return
	}
	verification, err := h.Svc.Submit(c.Request.Context(), userID, enums.VerificationMethodDocument, input.OrganizationEmail)
	if err != nil {
		h.respondError(c, err)
		return
	}
	responses.Success(c, http.StatusOK, "organization verification submitted successfully", verification)
}

func (h *OrganizationVerificationHandler) GetMyVerification(c *gin.Context) {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	verification, err := h.Svc.GetMy(c.Request.Context(), userID)
	if err != nil {
		h.respondError(c, err)
		return
	}
	h.respondVerification(c, http.StatusOK, "organization verification retrieved successfully", verification)
}

func (h *OrganizationVerificationHandler) UploadDocument(c *gin.Context) {
	userID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	file, err := c.FormFile("document")
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "verification document is required")
		return
	}
	verification, err := h.Svc.UploadDocument(c.Request.Context(), userID, file)
	if err != nil {
		h.respondError(c, err)
		return
	}
	h.respondVerification(c, http.StatusOK, "verification document uploaded successfully", verification)
}

func (h *OrganizationVerificationHandler) List(c *gin.Context) {
	page, pageSize := parseVerificationPagination(c)
	status := enums.OrganizationVerificationStatus(strings.ToLower(strings.TrimSpace(c.Query("status"))))
	items, total, err := h.Svc.List(c.Request.Context(), status, page, pageSize)
	if err != nil {
		h.respondError(c, err)
		return
	}
	responses.SuccessWithPagination(c, http.StatusOK, "organization verifications retrieved successfully", items, responses.CalculatePagination(int64(page), int64(pageSize), total))
}

func (h *OrganizationVerificationHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid verification ID")
		return
	}
	verification, err := h.Svc.GetByID(c.Request.Context(), id)
	if err != nil {
		h.respondError(c, err)
		return
	}
	h.respondVerification(c, http.StatusOK, "organization verification retrieved successfully", verification)
}

func (h *OrganizationVerificationHandler) Review(c *gin.Context) {
	reviewerID, err := getUserIDFromContext(c)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, err.Error())
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid verification ID")
		return
	}
	var input reviewOrganizationVerificationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid request payload")
		return
	}
	if err := h.Svc.Review(c.Request.Context(), id, reviewerID, input.Status, input.RejectionReason, input.ReviewNotes); err != nil {
		h.respondError(c, err)
		return
	}
	responses.Success(c, http.StatusOK, "organization verification reviewed successfully", nil)
}

func (h *OrganizationVerificationHandler) respondVerification(c *gin.Context, status int, message string, verification *models.OrganizationVerification) {
	documentURL, err := h.Svc.GetDocumentURL(c.Request.Context(), verification.DocumentObjectKey)
	if err != nil {
		h.Log.Error("failed to create verification document URL: %v", err)
		responses.Error(c, http.StatusInternalServerError, "failed to retrieve verification document")
		return
	}
	responses.Success(c, status, message, gin.H{"verification": verification, "document_url": documentURL})
}

func (h *OrganizationVerificationHandler) respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrOrganizationVerificationNotFound):
		responses.Error(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrOrganizationEmailAlreadyRegistered):
		responses.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrInvalidOrganizationVerification), errors.Is(err, services.ErrInvalidVerificationState):
		responses.Error(c, http.StatusBadRequest, err.Error())
	default:
		h.Log.Error("organization verification request failed: %v", err)
		responses.Error(c, http.StatusInternalServerError, "organization verification request failed")
	}
}

func parseVerificationPagination(c *gin.Context) (int, int) {
	page, pageSize := 1, 10
	if value, err := strconv.Atoi(c.Query("page")); err == nil && value > 0 {
		page = value
	}
	if value, err := strconv.Atoi(c.Query("page_size")); err == nil && value > 0 && value <= 100 {
		pageSize = value
	}
	return page, pageSize
}
