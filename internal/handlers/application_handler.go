package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

// ApplicationHandler exposes endpoints for applying and reviewing applications
type ApplicationHandler struct {
	Svc *services.InternshipApplicationService
	Log *logger.Logger
}

func NewApplicationHandler(
	svc *services.InternshipApplicationService,
	l *logger.Logger,
) *ApplicationHandler {
	return &ApplicationHandler{
		Svc: svc,
		Log: l,
	}
}

// Student: Apply to an internship
func (h *ApplicationHandler) Apply(c *gin.Context) {
	uid, ok := getAuthenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}

	internshipID, err := uuid.Parse(c.Param("internship_id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid internship id")
		return
	}

	app := &models.InternshipApplication{
		InternshipID: internshipID,
		StudentID:    uid,
	}

	if err := h.Svc.CreateApplication(c.Request.Context(), app); err != nil {
		h.handleApplicationError(c, err)
		return
	}

	responses.Success(c, http.StatusCreated, "application submitted successfully", app)
}

// Student: list own applications
func (h *ApplicationHandler) ListOwn(c *gin.Context) {
	uid, ok := getAuthenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}

	page, pageSize := parsePaginationParams(c)
	filter := services.StudentApplicationFilter{
		Status:   services.StudentApplicationStatusFilter(strings.TrimSpace(c.Query("status"))),
		Page:     page,
		PageSize: pageSize,
	}
	apps, total, err := h.Svc.ListByStudent(c.Request.Context(), uid, filter)
	if err != nil {
		h.handleApplicationError(c, err)
		return
	}

	responses.SuccessWithPagination(
		c,
		http.StatusOK,
		"applications",
		apps,
		responses.CalculatePagination(int64(page), int64(pageSize), total),
	)
}

// Withdraw allows a student to withdraw their own active application.
func (h *ApplicationHandler) Withdraw(c *gin.Context) {
	studentProfileID, ok := getProfileID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid student profile context")
		return
	}

	applicationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid application id")
		return
	}

	if err := h.Svc.WithdrawApplication(c.Request.Context(), applicationID, studentProfileID); err != nil {
		h.handleApplicationError(c, err)
		return
	}

	responses.Success(c, http.StatusOK, "application withdrawn successfully", nil)
}

// ListForInternship lists applications for an internship owned by the employer.
func (h *ApplicationHandler) ListForInternship(c *gin.Context) {
	employerID, ok := getAuthenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}

	internshipIDStr := c.Param("internship_id")
	if internshipIDStr == "" {
		internshipIDStr = c.Param("id")
	}

	internshipID, err := uuid.Parse(internshipIDStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid internship id")
		return
	}

	page, pageSize := parsePaginationParams(c)
	apps, total, err := h.Svc.ListByInternship(c.Request.Context(), internshipID, employerID, page, pageSize)
	if err != nil {
		h.handleApplicationError(c, err)
		return
	}

	responses.SuccessWithPagination(
		c,
		http.StatusOK,
		"applications",
		apps,
		responses.CalculatePagination(int64(page), int64(pageSize), total),
	)
}

// ListForRecruiter lists applications across internships owned by the
// authenticated employer, with optional candidate, internship, and status
// filters.
func (h *ApplicationHandler) ListForRecruiter(c *gin.Context) {
	employerID, ok := getAuthenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}

	filter, err := parseRecruiterApplicationFilter(c)
	if err != nil {
		h.handleApplicationError(c, err)
		return
	}

	apps, total, err := h.Svc.ListForRecruiter(c.Request.Context(), employerID, filter)
	if err != nil {
		h.handleApplicationError(c, err)
		return
	}

	responses.SuccessWithPagination(
		c,
		http.StatusOK,
		"applications",
		apps,
		responses.CalculatePagination(int64(filter.Page), int64(filter.PageSize), total),
	)
}

// GetForRecruiter returns one application only when its internship belongs to
// the authenticated employer.
func (h *ApplicationHandler) GetForRecruiter(c *gin.Context) {
	employerID, ok := getAuthenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}

	applicationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid application id")
		return
	}

	detail, err := h.Svc.GetForRecruiter(c.Request.Context(), applicationID, employerID)
	if err != nil {
		h.handleApplicationError(c, err)
		return
	}

	responses.Success(c, http.StatusOK, "application retrieved successfully", detail)
}

// Employer: update application status
func (h *ApplicationHandler) UpdateStatus(c *gin.Context) {
	employerID, ok := getAuthenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}

	appID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid application id")
		return
	}

	var payload struct {
		Status       models.InternshipApplicationStatus `json:"status"`
		EmployerNote *string                            `json:"employer_note"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}

	updated, err := h.Svc.UpdateStatus(c.Request.Context(), appID, employerID, payload.Status, payload.EmployerNote)
	if err != nil {
		h.handleApplicationError(c, err)
		return
	}

	responses.Success(c, http.StatusOK, "status updated successfully", updated)
}

func parseRecruiterApplicationFilter(c *gin.Context) (services.RecruiterApplicationFilter, error) {
	page, pageSize := parsePaginationParams(c)
	filter := services.RecruiterApplicationFilter{
		Query:    strings.TrimSpace(c.Query("q")),
		Status:   models.InternshipApplicationStatus(strings.TrimSpace(c.Query("status"))),
		Page:     page,
		PageSize: pageSize,
	}

	if internshipID := strings.TrimSpace(c.Query("internship_id")); internshipID != "" {
		parsedID, err := uuid.Parse(internshipID)
		if err != nil {
			return filter, fmt.Errorf("%w: invalid internship ID", services.ErrInvalidApplicationData)
		}
		filter.InternshipID = &parsedID
	}

	return filter, nil
}

func getProfileID(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get("profile_id")
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

func getAuthenticatedUserID(c *gin.Context) (uuid.UUID, bool) {
	uid, ok := c.Get("user_id")
	if !ok {
		return uuid.Nil, false
	}
	uidStr, ok := uid.(string)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(uidStr)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func parsePaginationParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 10
	}
	return page, pageSize
}

func (h *ApplicationHandler) handleApplicationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrApplicationNotFound):
		responses.Error(c, http.StatusNotFound, "application not found")
	case errors.Is(err, services.ErrInternshipNotFound):
		responses.Error(c, http.StatusNotFound, "internship not found")
	case errors.Is(err, services.ErrAlreadyApplied):
		responses.Error(c, http.StatusConflict, "already applied to this internship")
	case errors.Is(err, services.ErrInternshipNotActive):
		responses.Error(c, http.StatusBadRequest, "internship is not active or accepting applications")
	case errors.Is(err, services.ErrApplicationClosed):
		responses.Error(c, http.StatusBadRequest, "application deadline has passed")
	case errors.Is(err, services.ErrUnauthorizedAccess):
		responses.Error(c, http.StatusForbidden, "unauthorized to access or modify this application")
	case errors.Is(err, services.ErrInvalidStatusTransition):
		responses.Error(c, http.StatusBadRequest, "invalid application status transition")
	case errors.Is(err, services.ErrInvalidApplicationData):
		responses.Error(c, http.StatusBadRequest, err.Error())
	default:
		if h.Log != nil {
			h.Log.Error("application request failed", "error", err)
		}
		responses.Error(c, http.StatusInternalServerError, "internal server error")
	}
}
