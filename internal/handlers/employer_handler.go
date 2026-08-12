package handlers

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

// EmployerHandler exposes endpoints for employers to manage companies and jobs
type EmployerHandler struct {
	JobSvc  *services.JobService
	JobRepo *repositories.JobRepository
	Log     *logger.Logger
}

func NewEmployerHandler(js *services.JobService, jr *repositories.JobRepository, l *logger.Logger) *EmployerHandler {
	return &EmployerHandler{JobSvc: js, JobRepo: jr, Log: l}
}

// CreateJob allows an authenticated employer to create a job
func (h *EmployerHandler) CreateJob(c *gin.Context) {
	ctx := c.Request.Context()
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	employerIDStr, _ := uid.(string)
	employerID, err := uuid.Parse(employerIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	var payload models.Job
	if err := c.ShouldBindJSON(&payload); err != nil {
		log.Println("CreateJob: failed to bind JSON:", err)
		responses.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}
	payload.IssuedBy = employerID
	payload.CreatedAt = time.Now()
	payload.UpdatedAt = time.Now()
	if err := h.JobSvc.CreateJob(ctx, &payload); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to create job")
		return
	}
	responses.Success(c, http.StatusCreated, "job created", payload)
}

// ListJobs returns jobs created by the authenticated employer
func (h *EmployerHandler) ListJobs(c *gin.Context) {
	ctx := c.Request.Context()
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	employerIDStr, _ := uid.(string)
	employerID, err := uuid.Parse(employerIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	// will be changed in future to support pagination and filtering
	jobs, _, err := h.JobSvc.ListByEmployer(ctx, employerID, 1, 10)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list jobs")
		return
	}
	responses.Success(c, http.StatusOK, "jobs", jobs)
}

// GetJob returns a specific job by id ensuring it belongs to the employer
func (h *EmployerHandler) GetJob(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job id")
		return
	}
	job, err := h.JobSvc.GetJob(ctx, id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "job not found")
		return
	}
	// ensure ownership
	uid, _ := c.Get("user_id")
	employerIDStr, _ := uid.(string)
	employerID, _ := uuid.Parse(employerIDStr)
	if job.IssuedBy != employerID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	responses.Success(c, http.StatusOK, "job", job)
}

// UpdateJob updates an existing job (must be owned by employer)
func (h *EmployerHandler) UpdateJob(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job id")
		return
	}
	existing, err := h.JobSvc.GetJob(ctx, id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "job not found")
		return
	}
	uid, _ := c.Get("user_id")
	employerIDStr, _ := uid.(string)
	employerID, _ := uuid.Parse(employerIDStr)
	if existing.IssuedBy != employerID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	var payload models.Job
	if err := c.ShouldBindJSON(&payload); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}
	// update fields
	existing.Title = payload.Title
	existing.Description = payload.Description
	existing.Location = payload.Location
	existing.Remote = payload.Remote
	existing.SalaryMin = payload.SalaryMin
	existing.SalaryMax = payload.SalaryMax
	existing.IsActive = payload.IsActive
	existing.UpdatedAt = time.Now()
	if err := h.JobSvc.UpdateJob(ctx, existing); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to update job")
		return
	}
	responses.Success(c, http.StatusOK, "updated", existing)
}

// DeleteJob removes a job owned by the employer
func (h *EmployerHandler) DeleteJob(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job id")
		return
	}
	existing, err := h.JobSvc.GetJob(ctx, id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "job not found")
		return
	}
	uid, _ := c.Get("user_id")
	employerIDStr, _ := uid.(string)
	employerID, _ := uuid.Parse(employerIDStr)
	if existing.IssuedBy != employerID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	if err := h.JobSvc.DeleteJob(ctx, id); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to delete job")
		return
	}
	responses.Success(c, http.StatusOK, "deleted", nil)
}
