package middleware

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
)

func JWTAuthMiddleware(cfg *config.Config, l *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenStr string

		// 1. Try to read token from the HttpOnly cookie
		cookieToken, err := c.Cookie("access_token")
		if err == nil && cookieToken != "" {
			tokenStr = cookieToken
		} else {
			// 2. Fallback to Authorization header if cookie is missing (e.g. for API clients/Mobile)
			auth := c.GetHeader("Authorization")
			if auth != "" {
				parts := strings.Fields(auth)
				if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
					tokenStr = parts[1]
				}
			}
		}
		log.Println("JWTAuthMiddleware: tokenStr:", tokenStr)

		// 3. Reject if no token was found in either cookie or header
		if tokenStr == "" {
			log.Println("JWTAuthMiddleware: no token found in cookie or header")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "missing authorization token",
			})
			return
		}

		// 4. Parse and validate the JWT token
		parser := jwt.NewParser()
		tkn, err := parser.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			// Ensure HMAC signing method
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
			return []byte(cfg.JWT.AccessSecret), nil
		})

		if err != nil || !tkn.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid or expired token",
			})
			return
		}

		claims, ok := tkn.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid token claims",
			})
			return
		}

		sub, ok := claims["sub"].(string)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid subject in token",
			})
			return
		}

		// 5. Store user information in Gin context
		c.Set("user_id", sub)
		if role, ok := claims["role"].(string); ok {
			c.Set("role", role)
		}

		c.Next()
	}
}
