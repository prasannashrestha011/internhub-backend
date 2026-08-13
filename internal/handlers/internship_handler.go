package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type InternshipHandler struct{ service *services.InternshipService }

func NewInternshipHandler(service *services.InternshipService) *InternshipHandler {
	return &InternshipHandler{service: service}
}

func (h *InternshipHandler) SearchInternships(c *gin.Context) {
	filter := parseInternshipSearchFilter(c)
	internships, total, err := h.service.SearchInternships(c.Request.Context(), filter)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to fetch internships")
		return
	}
	responses.SuccessWithPagination(c, http.StatusOK, "internships", internships, responses.CalculatePagination(int64(filter.Page), int64(filter.PageSize), total))
}

func (h *InternshipHandler) GetInternship(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid internship ID format")
		return
	}
	internship, err := h.service.GetInternship(c.Request.Context(), id)
	if errors.Is(err, services.ErrInternshipNotFound) {
		responses.Error(c, http.StatusNotFound, "internship not found")
		return
	}
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to fetch internship")
		return
	}
	responses.Success(c, http.StatusOK, "internship", internship)
}

func (h *InternshipHandler) CreateInternship(c *gin.Context) {
	var internship models.Internship
	if err := c.ShouldBindJSON(&internship); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	internship.IssuedBy = userID
	if err := h.service.CreateInternship(c.Request.Context(), &internship); err != nil {
		writeInternshipError(c, err, "failed to create internship")
		return
	}
	responses.Success(c, http.StatusCreated, "internship created successfully", internship)
}

func (h *InternshipHandler) UpdateInternship(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid internship ID format")
		return
	}
	existing, err := h.service.GetInternship(c.Request.Context(), id)
	if err != nil {
		writeInternshipError(c, err, "failed to fetch internship")
		return
	}
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	if existing.IssuedBy != userID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	var internship models.Internship
	if err := c.ShouldBindJSON(&internship); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}
	internship.ID = id
	if err := h.service.UpdateInternship(c.Request.Context(), &internship); err != nil {
		writeInternshipError(c, err, "failed to update internship")
		return
	}
	responses.Success(c, http.StatusOK, "internship updated successfully", internship)
}

func (h *InternshipHandler) DeleteInternship(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid internship ID format")
		return
	}
	internship, err := h.service.GetInternship(c.Request.Context(), id)
	if err != nil {
		writeInternshipError(c, err, "failed to fetch internship")
		return
	}
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	if internship.IssuedBy != userID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	if err := h.service.DeleteInternship(c.Request.Context(), id); err != nil {
		writeInternshipError(c, err, "failed to delete internship")
		return
	}
	responses.Success(c, http.StatusOK, "internship deleted successfully", nil)
}

func (h *InternshipHandler) ListMyInternships(c *gin.Context) {
	userID, ok := authenticatedUserID(c)
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "invalid user context")
		return
	}
	page, pageSize := parseInternshipPagination(c)
	internships, total, err := h.service.ListByEmployer(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to fetch internships")
		return
	}
	responses.SuccessWithPagination(c, http.StatusOK, "internships", internships, responses.CalculatePagination(int64(page), int64(pageSize), total))
}

func authenticatedUserID(c *gin.Context) (uuid.UUID, bool) {
	raw, ok := c.Get("user_id")
	if !ok {
		return uuid.Nil, false
	}
	value, ok := raw.(string)
	if !ok {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(value)
	return id, err == nil
}
func writeInternshipError(c *gin.Context, err error, fallback string) {
	if errors.Is(err, services.ErrInternshipNotFound) {
		responses.Error(c, http.StatusNotFound, "internship not found")
	} else if errors.Is(err, services.ErrInvalidInternshipData) {
		responses.Error(c, http.StatusBadRequest, err.Error())
	} else {
		responses.Error(c, http.StatusInternalServerError, fallback)
	}
}
func parseInternshipSearchFilter(c *gin.Context) repositories.InternshipSearchFilter {
	page, pageSize := parseInternshipPagination(c)
	f := repositories.InternshipSearchFilter{Query: c.Query("q"), Location: c.Query("location"), WorkMode: c.Query("work_mode"), InternshipType: c.Query("internship_type"), Status: c.Query("status"), Page: page, PageSize: pageSize}
	if id, err := uuid.Parse(c.Query("employer_id")); err == nil {
		f.EmployerID = &id
	}
	if value := c.Query("is_active"); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			f.IsActive = &b
		}
	}
	if value := c.Query("min_stipend"); value != "" {
		if amount, err := strconv.ParseFloat(value, 64); err == nil {
			f.MinStipend = &amount
		}
	}
	if value := c.Query("exclude_expired"); value != "" {
		f.ExcludeExpired, _ = strconv.ParseBool(value)
	}
	return f
}
func parseInternshipPagination(c *gin.Context) (int, int) {
	page, pageSize := 1, 10
	if n, err := strconv.Atoi(c.Query("page")); err == nil && n > 0 {
		page = n
	}
	if n, err := strconv.Atoi(c.Query("page_size")); err == nil && n > 0 && n <= 100 {
		pageSize = n
	}
	return page, pageSize
}
