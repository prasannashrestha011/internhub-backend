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
	// For simplicity, fetch all users (in production use pagination)
	var users []map[string]interface{}
	rows, err := h.UserRepo.DB.Raw("SELECT id, email, full_name, role, created_at FROM users ORDER BY created_at DESC").Rows()
	if err != nil {
		responses.Error(c, http.StatusInternalServerError, "Failed to query users")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, email, fullName, role string
		var createdAt string
		_ = rows.Scan(&id, &email, &fullName, &role, &createdAt)
		users = append(users, map[string]interface{}{"id": id, "email": email, "full_name": fullName, "role": role, "created_at": createdAt})
	}

	responses.Success(c, http.StatusOK, "Users retrieved successfully", users)
}
