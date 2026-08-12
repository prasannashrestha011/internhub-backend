package middleware

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prasanna/student-job-portal/backend/internal/models"
)

// RequireRoles ensures the authenticated user has one of the allowed roles
func RequireRoles(allowed ...models.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		r, exists := c.Get("role")
		if !exists {
			log.Println("role not found in context")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "missing role"})
			return
		}
		roleStr, ok := r.(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "message": "invalid role format"})
			return
		}

		for _, a := range allowed {
			if roleStr == string(a) {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": "insufficient permissions"})
	}
}
