package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type JobHandler struct {
	service *services.JobService
}

func NewJobHandler(service *services.JobService) *JobHandler {
	return &JobHandler{service: service}
}

// SearchJobs handles public job discovery with multi-field filtering and pagination.
// GET /api/v1/jobs
func (h *JobHandler) SearchJobs(c *gin.Context) {
	filter := parseSearchFilter(c)

	jobs, total, err := h.service.SearchJobs(c.Request.Context(), filter)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to fetch jobs")
		return
	}

	pagination := responses.CalculatePagination(int64(filter.Page), int64(filter.PageSize), total)
	responses.SuccessWithPagination(c, http.StatusOK, "jobs", jobs, pagination)
}

// GetJob fetches a single job record by ID along with its associated company details.
// GET /api/v1/jobs/:id
func (h *JobHandler) GetJob(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job ID format")
		return
	}

	job, err := h.service.GetJob(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, services.ErrJobNotFound) {
			responses.Error(c, http.StatusNotFound, "job not found")
			return
		}
		responses.Error(c, http.StatusInternalServerError, "failed to fetch job details")
		return
	}

	responses.Success(c, http.StatusOK, "job", job)
}

// CreateJob handles the creation of a new job posting by an authenticated employer.
// POST /api/v1/jobs
func (h *JobHandler) CreateJob(c *gin.Context) {
	var job models.Job
	if err := c.ShouldBindJSON(&job); err != nil {
		log.Println("Error binding JSON:", err)
		responses.Error(c, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}

	// Extract authenticated employer ID from context (set by Auth Middleware)
	if raw, exists := c.Get("user_id"); exists {
		if uid, err := uuid.Parse(raw.(string)); err == nil {
			job.IssuedBy = uid
		}
	}

	if err := h.service.CreateJob(c.Request.Context(), &job); err != nil {
		if errors.Is(err, services.ErrInvalidJobData) {
			responses.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		responses.Error(c, http.StatusInternalServerError, "failed to create job posting")
		return
	}

	responses.Success(c, http.StatusCreated, "job created successfully", job)
}

// UpdateJob updates an existing job posting.
// PUT /api/v1/jobs/:id
func (h *JobHandler) UpdateJob(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job ID format")
		return
	}

	var job models.Job
	if err := c.ShouldBindJSON(&job); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}
	job.ID = id

	if err := h.service.UpdateJob(c.Request.Context(), &job); err != nil {
		if errors.Is(err, services.ErrJobNotFound) {
			responses.Error(c, http.StatusNotFound, "job not found")
			return
		}
		if errors.Is(err, services.ErrInvalidJobData) {
			responses.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		responses.Error(c, http.StatusInternalServerError, "failed to update job posting")
		return
	}

	responses.Success(c, http.StatusOK, "job updated successfully", job)
}

// DeleteJob soft-deletes or removes a job posting by ID.
// DELETE /api/v1/jobs/:id
func (h *JobHandler) DeleteJob(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job ID format")
		return
	}

	if err := h.service.DeleteJob(c.Request.Context(), id); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to delete job posting")
		return
	}

	responses.Success(c, http.StatusOK, "job deleted successfully", nil)
}

// ListCompanyJobs fetches all jobs belonging to a specific company workspace.
// GET /api/v1/companies/:company_id/jobs
func (h *JobHandler) ListCompanyJobs(c *gin.Context) {
	companyID, err := uuid.Parse(c.Param("company_id"))
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid company ID format")
		return
	}

	page, pageSize := parsePagination(c)
	jobs, total, err := h.service.ListByCompany(c.Request.Context(), companyID, page, pageSize)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to fetch company jobs")
		return
	}

	pagination := responses.CalculatePagination(int64(page), int64(pageSize), total)
	responses.SuccessWithPagination(c, http.StatusOK, "jobs", jobs, pagination)
}

// Helper function to extract query parameters into JobSearchFilter
func parseSearchFilter(c *gin.Context) repositories.JobSearchFilter {
	page, pageSize := parsePagination(c)

	filter := repositories.JobSearchFilter{
		Query:           c.Query("q"),
		Location:        c.Query("location"),
		JobType:         c.Query("job_type"),
		WorkMode:        c.Query("work_mode"),
		ExperienceLevel: c.Query("experience_level"),
		Status:          c.Query("status"),
		Page:            page,
		PageSize:        pageSize,
	}

	if employerIDStr := c.Query("employer_id"); employerIDStr != "" {
		if id, err := uuid.Parse(employerIDStr); err == nil {
			filter.EmployerID = &id
		}
	}

	if remoteStr := c.Query("remote"); remoteStr != "" {
		if b, err := strconv.ParseBool(remoteStr); err == nil {
			filter.Remote = &b
		}
	}

	if isActiveStr := c.Query("is_active"); isActiveStr != "" {
		if b, err := strconv.ParseBool(isActiveStr); err == nil {
			filter.IsActive = &b
		}
	}

	if minSalaryStr := c.Query("min_salary"); minSalaryStr != "" {
		if f, err := strconv.ParseFloat(minSalaryStr, 64); err == nil {
			filter.MinSalary = &f
		}
	}

	if excludeExpiredStr := c.Query("exclude_expired"); excludeExpiredStr != "" {
		if b, err := strconv.ParseBool(excludeExpiredStr); err == nil {
			filter.ExcludeExpired = b
		}
	}

	return filter
}

// Helper function to parse pagination defaults
func parsePagination(c *gin.Context) (page int, pageSize int) {
	page = 1
	pageSize = 10

	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 0 {
		page = p
	}
	if ps, err := strconv.Atoi(c.Query("page_size")); err == nil && ps > 0 && ps <= 100 {
		pageSize = ps
	}

	return page, pageSize
}
