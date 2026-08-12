package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

// ApplicationHandler exposes endpoints for applying and reviewing applications
type ApplicationHandler struct {
	Svc        *services.ApplicationService
	Repo       *repositories.JobApplicationRepository
	JobRepo    *repositories.JobRepository
	AnsRepo    *repositories.ApplicationAnswerRepository
	StatusRepo *repositories.ApplicationStatusHistoryRepository
	Log        *logger.Logger
}

func NewApplicationHandler(svc *services.ApplicationService, repo *repositories.JobApplicationRepository, jr *repositories.JobRepository, ans *repositories.ApplicationAnswerRepository, status *repositories.ApplicationStatusHistoryRepository, l *logger.Logger) *ApplicationHandler {
	return &ApplicationHandler{Svc: svc, Repo: repo, JobRepo: jr, AnsRepo: ans, StatusRepo: status, Log: l}
}

// Student: Apply to a job (multipart form, optional resume file field "resume")
func (h *ApplicationHandler) Apply(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	studentIDStr, _ := uid.(string)
	studentID, err := uuid.Parse(studentIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	jobIDStr := c.Param("job_id")
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job id")
		return
	}
	cover := c.PostForm("cover_note")
	resume, _ := c.FormFile("resume")
	// optional answers JSON in form field `answers` (array of {question,answer})
	var answers []models.ApplicationAnswer
	if ansStr := c.PostForm("answers"); ansStr != "" {
		if err := json.Unmarshal([]byte(ansStr), &answers); err != nil {
			responses.Error(c, http.StatusBadRequest, "invalid answers payload")
			return
		}
	}
	// validation
	maxResume := int64(5 * 1024 * 1024)
	allowed := map[string]bool{"application/pdf": true, "application/msword": true, "application/vnd.openxmlformats-officedocument.wordprocessingml.document": true}
	app, err := h.Svc.Apply(c.Request.Context(), jobID, studentID, cover, resume, answers, maxResume, allowed, h.AnsRepo, h.StatusRepo)
	if err != nil {
		h.Log.Error("apply failed: %v", err)
		responses.Error(c, http.StatusInternalServerError, "failed to apply")
		return
	}
	responses.Success(c, http.StatusCreated, "applied", app)
}

// Student: list own applications
func (h *ApplicationHandler) ListOwn(c *gin.Context) {
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	studentIDStr, _ := uid.(string)
	studentID, err := uuid.Parse(studentIDStr)
	if err != nil {
		responses.Error(c, http.StatusUnauthorized, "invalid user id")
		return
	}
	out, err := h.Repo.ListByStudent(studentID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list applications")
		return
	}
	responses.Success(c, http.StatusOK, "applications", out)
}

// Employer: list applications for a job
func (h *ApplicationHandler) ListForJob(c *gin.Context) {
	ctx := c.Request.Context()
	jobIDStr := c.Param("job_id")
	if jobIDStr == "" {
		jobIDStr = c.Param("id")
	}
	jobID, err := uuid.Parse(jobIDStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid job id")
		return
	}
	// ensure job belongs to employer
	job, err := h.JobRepo.GetByID(ctx, jobID)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "job not found")
		return
	}
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	employerIDStr, _ := uid.(string)
	employerID, _ := uuid.Parse(employerIDStr)
	if job.IssuedBy != employerID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	out, err := h.Repo.ListByJob(jobID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list applications")
		return
	}
	responses.Success(c, http.StatusOK, "applications", out)
}

// ListAnswers returns question/answer pairs for a given application. Employer or the applicant may view.
func (h *ApplicationHandler) ListAnswers(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid application id")
		return
	}
	app, err := h.Repo.GetByID(id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "application not found")
		return
	}
	// ownership check: student or employer
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, _ := uuid.Parse(userIDStr)
	roleI, _ := c.Get("role")
	roleStr, _ := roleI.(string)
	if roleStr == "student" {
		if app.StudentID != userID {
			responses.Error(c, http.StatusForbidden, "not allowed")
			return
		}
	} else if roleStr == "employer" {
		job, err := h.JobRepo.GetByID(ctx, app.JobID)
		if err != nil {
			responses.Error(c, http.StatusNotFound, "job not found")
			return
		}
		if job.IssuedBy != userID {
			responses.Error(c, http.StatusForbidden, "not allowed")
			return
		}
	} else {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	out, err := h.AnsRepo.ListByApplication(app.ID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list answers")
		return
	}
	responses.Success(c, http.StatusOK, "answers", out)
}

// ListHistory returns application status history; accessible to student and employer (owner)
func (h *ApplicationHandler) ListHistory(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid application id")
		return
	}
	app, err := h.Repo.GetByID(id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "application not found")
		return
	}
	// ownership check
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	userIDStr, _ := uid.(string)
	userID, _ := uuid.Parse(userIDStr)
	roleI, _ := c.Get("role")
	roleStr, _ := roleI.(string)
	if roleStr == "student" {
		if app.StudentID != userID {
			responses.Error(c, http.StatusForbidden, "not allowed")
			return
		}
	} else if roleStr == "employer" {
		job, err := h.JobRepo.GetByID(ctx, app.JobID)
		if err != nil {
			responses.Error(c, http.StatusNotFound, "job not found")
			return
		}
		if job.IssuedBy != userID {
			responses.Error(c, http.StatusForbidden, "not allowed")
			return
		}
	} else {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	out, err := h.StatusRepo.ListByApplication(app.ID)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to list history")
		return
	}
	responses.Success(c, http.StatusOK, "history", out)
}

// Employer: update application status
func (h *ApplicationHandler) UpdateStatus(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		responses.Error(c, http.StatusBadRequest, "invalid payload")
		return
	}
	// verify ownership: application -> job -> employer
	app, err := h.Repo.GetByID(id)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "application not found")
		return
	}
	job, err := h.JobRepo.GetByID(ctx, app.JobID)
	if err != nil {
		responses.Error(c, http.StatusNotFound, "job not found")
		return
	}
	uid, ok := c.Get("user_id")
	if !ok {
		responses.Error(c, http.StatusUnauthorized, "missing user in context")
		return
	}
	employerIDStr, _ := uid.(string)
	employerID, _ := uuid.Parse(employerIDStr)
	if job.IssuedBy != employerID {
		responses.Error(c, http.StatusForbidden, "not allowed")
		return
	}
	// proceed to update via service so history is recorded
	updated, err := h.Svc.UpdateStatus(id, payload.Status)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "failed to update status")
		return
	}
	responses.Success(c, http.StatusOK, "updated", updated)
}
