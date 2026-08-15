package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
)

type AdminHandler struct {
	UserRepo *repositories.UserRepository
	Logger   *logger.Logger
}

func NewAdminHandler(repo *repositories.UserRepository, l *logger.Logger) *AdminHandler {
	return &AdminHandler{UserRepo: repo, Logger: l}
}

// ListUsers returns a minimal list of users (id, email, full_name, role)
func (h *AdminHandler) ListUsers(c *gin.Context) {
	page, pageSize := parsePagination(c)

	users, total, err := h.UserRepo.List(page, pageSize)
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to query users")
		return
	}
	responses.SuccessWithPagination(c, http.StatusOK, "users list", users, responses.CalculatePagination(int64(page), int64(pageSize), total))
}
