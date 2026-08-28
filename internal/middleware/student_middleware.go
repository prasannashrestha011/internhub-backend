package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	// adjust to your actual repo package
)

// ResolveStudentIDMiddleware looks up student_id from user_id (set by JWTAuthMiddleware)
// and stores it in context. Must run AFTER JWTAuthMiddleware.
func ResolveStudentProfileIDMiddleware(repo *repositories.StudentRepository, l *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		userIDStr, ok := c.Get("user_id")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "missing user context",
			})
			return
		}
		userID, err := uuid.Parse(userIDStr.(string))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid user id",
			})
			return
		}

		studentID, err := repo.ResolveProfileID(userID)
		if err != nil {
			l.Error("failed to resolve student_id for user_id", "user_id", userID, "err", err)
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "student profile not found",
			})
			return
		}

		c.Set("profile_id", studentID)
		c.Next()
	}
}
