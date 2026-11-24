// Package routes assembles HTTP routes and their dependencies.
package routes

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/handlers"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/middleware"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
)

// Register creates application dependencies and registers all API routes.
func Register(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.Logger, minioClient *minio.Client) {
	userRepo := repositories.NewUserRepository(db)
	studentRepo := repositories.NewStudentRepository(db)
	recruiterProfileRepo := repositories.NewRecruiterProfileRepository(db)
	internshipRepo := repositories.NewInternshipRepository(db)
	appRepo := repositories.NewInternshipApplicationRepository(db)

	authSvc := services.NewAuthService(userRepo, cfg, log)
	studentSvc := services.NewStudentService(studentRepo, minioClient, cfg, log)
	recruiterProfileSvc := services.NewRecruiterProfileService(recruiterProfileRepo, minioClient, cfg.MinIO.ProfileBucket)
	appSvc := services.NewInternshipApplicationService(appRepo, internshipRepo, studentRepo, minioClient, cfg.MinIO.StudentDocBucket, recruiterProfileSvc, userRepo, services.SMTPVerificationSender{Config: cfg.Email}, log)
	internshipSvc := services.NewInternshipService(internshipRepo, recruiterProfileRepo, recruiterProfileSvc)

	authHandler := handlers.NewAuthHandler(authSvc, log)
	studentHandler := handlers.NewStudentHandler(studentSvc)
	recruiterProfileHandler := handlers.NewRecruiterProfileHandler(recruiterProfileSvc, log)
	internshipHandler := handlers.NewInternshipHandler(internshipSvc)
	appHandler := handlers.NewApplicationHandler(appSvc, log)

	router.GET("/health", healthCheck)

	v1 := router.Group("/api/v1")
	v1.GET("/health", healthCheck)

	authGroup := v1.Group("/auth")
	authGroup.Use(middleware.RateLimitMiddleware())
	authGroup.POST("/register/student", authHandler.RegisterStudent)
	authGroup.POST("/register/employer", authHandler.RegisterEmployer)
	authGroup.POST("/verify-email", authHandler.VerifyEmail)
	authGroup.POST("/resend-verification", authHandler.ResendVerification)
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/refresh", authHandler.Refresh)
	authGroup.POST("/logout", authHandler.Logout)
	v1.GET("/auth/me", middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent, models.RoleEmployer), authHandler.Me)

	recruiterGroup := v1.Group("/recruiters")
	recruiterGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleEmployer))
	profile := recruiterGroup.Group("/me/profile")
	profile.GET("", recruiterProfileHandler.GetMyProfile)
	profile.POST("/logo", recruiterProfileHandler.UploadOrganizationLogo)
	profile.PUT("", recruiterProfileHandler.UpsertMyProfile)
	profile.DELETE("", recruiterProfileHandler.DeleteMyProfile)
	recruiterGroup.GET("/applications", appHandler.ListForRecruiter)
	recruiterGroup.GET("/applications/:id", appHandler.GetForRecruiter)
	recruiterGroup.PUT("/applications/:id/status", appHandler.UpdateStatus)

	studentGroup := v1.Group("/students")
	me := studentGroup.Group("/me")
	me.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent))

	me.GET("/profile", studentHandler.GetProfile)
	me.POST("/profile", studentHandler.UpsertProfile)
	me.POST("/profile/picture", studentHandler.UploadProfilePicture)
	me.GET("/resume", studentHandler.GetResume)
	me.PUT("/resume", studentHandler.UploadResume)
	me.DELETE("/resume", studentHandler.DeleteResume)

	profileRequired := me.Group("")
	profileRequired.Use(middleware.ResolveStudentProfileIDMiddleware(studentRepo, log))
	profileRequired.GET("/stats", studentHandler.GetApplicationStats)
	profileRequired.GET("/applications", appHandler.ListOwn)
	profileRequired.GET("/applications/internships/:internship_id", appHandler.FindOwnForInternship)
	profileRequired.GET("/applications/stats", studentHandler.GetApplicationStats)
	profileRequired.PATCH("/applications/:id/withdraw", appHandler.Withdraw)

	/* public routes*/
	internships := v1.Group("/internships")
	internships.GET("", internshipHandler.SearchInternships)
	internships.GET("/:id", internshipHandler.GetInternship)

	studentInternships := v1.Group("/internships")
	studentInternships.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent))
	studentInternships.POST("/:internship_id/apply", appHandler.Apply)

	recruiterInternships := recruiterGroup.Group("/me/internships")
	recruiterInternships.GET("", internshipHandler.ListMyInternships)
	recruiterInternships.POST("", internshipHandler.CreateInternship)
	recruiterInternships.PUT("/:id", internshipHandler.UpdateInternship)
	recruiterInternships.DELETE("/:id", internshipHandler.DeleteInternship)
	recruiterInternships.GET("/:id/applications", appHandler.ListForInternship)

}

func healthCheck(c *gin.Context) {
	responses.Success(c, http.StatusOK, "Health check passed", gin.H{
		"status": "ok", "timestamp": time.Now(),
	})
}
