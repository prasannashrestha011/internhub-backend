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
	organizationVerificationRepo := repositories.NewOrganizationVerificationRepository(db)
	internshipRepo := repositories.NewInternshipRepository(db)
	appRepo := repositories.NewInternshipApplicationRepository(db)
	interviewRepo := repositories.NewInterviewRepository(db)

	authSvc := services.NewAuthService(userRepo, cfg, log)
	studentSvc := services.NewStudentService(studentRepo, minioClient, cfg, log)
	appSvc := services.NewInternshipApplicationService(appRepo, internshipRepo, studentRepo, minioClient, cfg.MinIO.StudentDocBucket)
	internshipSvc := services.NewInternshipService(internshipRepo, recruiterProfileRepo)
	interviewSvc := services.NewInterviewService(interviewRepo)
	recruiterProfileSvc := services.NewRecruiterProfileService(recruiterProfileRepo, minioClient, cfg.MinIO.ProfileBucket)
	organizationVerificationSvc := services.NewOrganizationVerificationService(organizationVerificationRepo, recruiterProfileRepo, minioClient, cfg.MinIO.CompanyDocBucket)

	authHandler := handlers.NewAuthHandler(authSvc, log)
	adminHandler := handlers.NewAdminHandler(userRepo, log)
	studentHandler := handlers.NewStudentHandler(studentSvc, studentRepo, cfg, log)
	recruiterProfileHandler := handlers.NewRecruiterProfileHandler(recruiterProfileSvc, log)
	organizationVerificationHandler := handlers.NewOrganizationVerificationHandler(organizationVerificationSvc, log)
	internshipHandler := handlers.NewInternshipHandler(internshipSvc)
	appHandler := handlers.NewApplicationHandler(appSvc, log)
	interviewHandler := handlers.NewInterviewHandler(interviewSvc, interviewRepo, internshipRepo)

	router.GET("/health", healthCheck)

	v1 := router.Group("/api/v1")
	v1.GET("/health", healthCheck)

	authGroup := v1.Group("/auth")
	authGroup.Use(middleware.RateLimitMiddleware())
	authGroup.POST("/register/student", authHandler.RegisterStudent)
	authGroup.POST("/register/employer", authHandler.RegisterEmployer)
	authGroup.POST("/login", authHandler.Login)
	authGroup.POST("/refresh", authHandler.Refresh)
	authGroup.POST("/logout", authHandler.Logout)
	authGroup.GET("/me", middleware.JWTAuthMiddleware(cfg, log), authHandler.Me)

	adminGroup := v1.Group("/admin")
	adminGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleAdmin))
	adminGroup.GET("/users", adminHandler.ListUsers)
	adminGroup.GET("/organization-verifications", organizationVerificationHandler.List)
	adminGroup.GET("/organization-verifications/:id", organizationVerificationHandler.GetByID)
	adminGroup.PUT("/organization-verifications/:id/review", organizationVerificationHandler.Review)

	recruiterGroup := v1.Group("/recruiters")
	recruiterGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleEmployer))
	profile := recruiterGroup.Group("/me/profile")
	profile.GET("", recruiterProfileHandler.GetMyProfile)
	profile.POST("/logo", recruiterProfileHandler.UploadOrganizationLogo)
	profile.PUT("", recruiterProfileHandler.UpsertMyProfile)
	profile.DELETE("", recruiterProfileHandler.DeleteMyProfile)
	verification := recruiterGroup.Group("/me/verification")
	verification.GET("", organizationVerificationHandler.GetMyVerification)
	verification.POST("", organizationVerificationHandler.SubmitVerification)
	verification.POST("/document", organizationVerificationHandler.UploadDocument)
	recruiterGroup.GET("/applications", appHandler.ListForRecruiter)
	recruiterGroup.GET("/applications/:id", appHandler.GetForRecruiter)
	recruiterGroup.PUT("/applications/:id/status", appHandler.UpdateStatus)
	interviews := recruiterGroup.Group("/interviews")
	interviews.POST("", interviewHandler.Schedule)
	interviews.GET("", interviewHandler.ListEmployer)
	interviews.DELETE("/:id", interviewHandler.Cancel)

	studentGroup := v1.Group("/students")
	me := studentGroup.Group("/me")
	me.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.ResolveStudentProfileIDMiddleware(studentRepo, log), middleware.RequireRoles(models.RoleStudent))

	me.GET("/profile", studentHandler.GetProfile)
	me.GET("/stats", studentHandler.GetApplicationStats)
	me.POST("/profile", studentHandler.UpsertProfile)
	me.POST("/documents", studentHandler.UploadDocument)
	me.GET("/documents", studentHandler.ListDocuments)
	me.POST("/documents/:id/default", studentHandler.SetDefaultDocument)
	me.DELETE("/documents/:id", studentHandler.DeleteDocument)
	me.GET("/applications", appHandler.ListOwn)
	me.GET("/applications/internships/:internship_id", appHandler.FindOwnForInternship)
	me.GET("/applications/stats", studentHandler.GetApplicationStats)
	me.PATCH("/applications/:id/withdraw", appHandler.Withdraw)
	me.GET("/interviews", interviewHandler.ListStudent)
	me.POST("/interviews/:id/respond", interviewHandler.Respond)

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
