package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

type InterviewHandler struct {
	Svc     *services.InterviewService
	Repo    *repositories.InterviewRepository
	JobRepo *repositories.JobRepository
}

func NewInterviewHandler(svc *services.InterviewService, repo *repositories.InterviewRepository, jobRepo *repositories.JobRepository) *InterviewHandler {
	return &InterviewHandler{Svc: svc, Repo: repo, JobRepo: jobRepo}
}

// Employer: schedule an interview
func (h *InterviewHandler) Schedule(c *gin.Context) {
	ctx := c.Request.Context()
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	employerStr, _ := uid.(string)
	employerID, err := uuid.Parse(employerStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	var payload models.Interview
	if err := c.ShouldBindJSON(&payload); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}
	// ensure employer owns the job
	job, err := h.JobRepo.GetByID(ctx, payload.JobID)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "job not found")
		return
	}
	if job.IssuedBy != employerID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	payload.EmployerID = employerID
	if payload.ScheduledAt.IsZero() {
		payload.ScheduledAt = time.Now().Add(24 * time.Hour)
	}
	if err := h.Svc.ScheduleInterview(&payload); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to schedule")
		return
	}
	responses.Success(c, http.StatusCreated, "scheduled", payload)
}

// Employer: list interviews for employer
func (h *InterviewHandler) ListEmployer(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	employerStr, _ := uid.(string)
	employerID, err := uuid.Parse(employerStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	out, err := h.Svc.ListByEmployer(employerID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list")
		return
	}
	responses.Success(c, http.StatusOK, "interviews", out)
}

// Student: list own interviews
func (h *InterviewHandler) ListStudent(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	studentStr, _ := uid.(string)
	studentID, err := uuid.Parse(studentStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	out, err := h.Svc.ListByStudent(studentID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list")
		return
	}
	responses.Success(c, http.StatusOK, "interviews", out)
}

// Student: respond to interview (accept/decline)
func (h *InterviewHandler) Respond(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	var payload struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	studentStr, _ := uid.(string)
	studentID, err := uuid.Parse(studentStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	i, err := h.Svc.GetInterview(id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "interview not found")
		return
	}
	if i.StudentID != studentID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	switch payload.Action {
	case "accept":
		i.Status = "accepted"
	case "decline":
		i.Status = "declined"
	default:
		responses.Error(c, http.StatusBadRequest, "invalid action")
		return
	}
	if _, err := h.Svc.UpdateStatus(id, i.Status); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to update")
		return
	}
	responses.Success(c, http.StatusOK, "updated", i)
}

// Employer: cancel interview
func (h *InterviewHandler) Cancel(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	employerStr, _ := uid.(string)
	employerID, err := uuid.Parse(employerStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	i, err := h.Svc.GetInterview(id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "interview not found")
		return
	}
	if i.EmployerID != employerID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	if err := h.Svc.CancelInterview(id); err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to cancel")
		return
	}
	responses.Success(c, http.StatusOK, "cancelled", nil)
}
