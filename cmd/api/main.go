package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/handlers"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/middleware"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/responses"
	"github.com/prasanna/student-job-portal/backend/internal/services"
	"github.com/prasanna/student-job-portal/backend/internal/storage"
)

func main() {
	// 1. Load Configuration & Logger
	cfg, err := config.Load()
	if err != nil {
		panic(fmt.Sprintf("Failed to load configuration: %v", err))
	}

	log := logger.New(cfg.App.Env)
	log.Info("Starting Student Job Portal Backend [Env: %s]", cfg.App.Env)

	// 2. Connect Database & Run Migrations
	db, err := database.Connect(&cfg.Database, log)
	if err != nil {
		log.Fatal("Failed to connect to database: %v", err)
	}
	defer database.Close(db)

	if err := autoMigrate(db); err != nil {
		log.Fatal("AutoMigrate failed: %v", err)
	}

	// 3. Seed Initial Data
	seedAdminUser(db, cfg, log)

	// 4. Connect MinIO Storage
	minioClient, err := storage.ConnectMinIO(&cfg.MinIO, log)
	if err != nil {
		log.Fatal("Failed to connect to MinIO: %v", err)
	}

	// 5. Initialize Engine & Register Routes
	router := initRouter(cfg)
	setupRoutes(router, db, cfg, log, minioClient)

	// 6. Start Server
	startServer(router, cfg, log)
}

// ============================================================================
// ROUTE CONFIGURATION & DEPENDENCY INJECTION
// ============================================================================

func setupRoutes(router *gin.Engine, db *gorm.DB, cfg *config.Config, log *logger.Logger, minioClient *minio.Client) {
	// --- 1. Repositories ---
	userRepo := repositories.NewUserRepository(db)
	studentRepo := repositories.NewStudentRepository(db)
	employerProfileRepo := repositories.NewEmployerProfileRepository(db)
	organizationVerificationRepo := repositories.NewOrganizationVerificationRepository(db)
	jobRepo := repositories.NewJobRepository(db)
	appRepo := repositories.NewJobApplicationRepository(db)
	ansRepo := repositories.NewApplicationAnswerRepository(db)
	appHistoryRepo := repositories.NewApplicationStatusHistoryRepository(db)
	interviewRepo := repositories.NewInterviewRepository(db)

	// --- 2. Services ---
	authSvc := services.NewAuthService(userRepo, cfg, log)
	studentSvc := services.NewStudentService(studentRepo, minioClient, cfg, log)
	appSvc := services.NewApplicationService(appRepo, appHistoryRepo, minioClient, cfg, log)
	interviewSvc := services.NewInterviewService(interviewRepo, jobRepo, appSvc)
	employerProfileSvc := services.NewEmployerProfileService(employerProfileRepo, minioClient, cfg.MinIO.ProfileBucket)
	organizationVerificationSvc := services.NewOrganizationVerificationService(organizationVerificationRepo, employerProfileRepo, minioClient, cfg.MinIO.CompanyDocBucket)

	// --- 3. Handlers ---
	authHandler := handlers.NewAuthHandler(authSvc, log)
	adminHandler := handlers.NewAdminHandler(userRepo, log)
	studentHandler := handlers.NewStudentHandler(studentSvc, studentRepo, cfg, log)
	employerProfileHandler := handlers.NewEmployerProfileHandler(employerProfileSvc, log)
	organizationVerificationHandler := handlers.NewOrganizationVerificationHandler(organizationVerificationSvc, log)
	appHandler := handlers.NewApplicationHandler(appSvc, appRepo, jobRepo, ansRepo, appHistoryRepo, log)
	interviewHandler := handlers.NewInterviewHandler(interviewSvc, interviewRepo, jobRepo)

	// --- 4. API v1 Router Groups ---
	v1 := router.Group("/api/v1")
	{
		// Health Check
		v1.GET("/health", healthCheckHandler)

		// Authentication Routes
		authGroup := v1.Group("/auth")
		authGroup.Use(middleware.RateLimitMiddleware())
		{
			authGroup.POST("/register/student", authHandler.RegisterStudent)
			authGroup.POST("/register/employer", authHandler.RegisterEmployer)
			authGroup.POST("/login", authHandler.Login)
			authGroup.POST("/refresh", authHandler.Refresh)
			authGroup.POST("/logout", authHandler.Logout)
			authGroup.GET("/me", middleware.JWTAuthMiddleware(cfg, log), authHandler.Me)
		}

		// Admin Routes
		adminGroup := v1.Group("/admin")
		adminGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleAdmin))
		{
			adminGroup.GET("/users", adminHandler.ListUsers)
			adminGroup.GET("/organization-verifications", organizationVerificationHandler.List)
			adminGroup.GET("/organization-verifications/:id", organizationVerificationHandler.GetByID)
			adminGroup.PUT("/organization-verifications/:id/review", organizationVerificationHandler.Review)
		}

		// Employer Routes
		employerGroup := v1.Group("/employers")
		employerGroup.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleEmployer))
		{
			// Profile
			profile := employerGroup.Group("/me/profile")
			{
				profile.GET("", employerProfileHandler.GetMyProfile)
				profile.POST("/logo", employerProfileHandler.UploadOrganizationLogo)
				profile.PUT("", employerProfileHandler.UpsertMyProfile)
				profile.DELETE("", employerProfileHandler.DeleteMyProfile)
			}
			verification := employerGroup.Group("/me/verification")
			{
				verification.GET("", organizationVerificationHandler.GetMyVerification)
				verification.POST("", organizationVerificationHandler.SubmitVerification)
				verification.POST("/document", organizationVerificationHandler.UploadDocument)
			}

			// Application Status Updates
			employerGroup.PUT("/applications/:id/status", appHandler.UpdateStatus)

			// Interviews
			interviews := employerGroup.Group("/interviews")
			{
				interviews.POST("", interviewHandler.Schedule)
				interviews.GET("", interviewHandler.ListEmployer)
				interviews.DELETE("/:id", interviewHandler.Cancel)
			}
		}

		// Student Routes
		studentGroup := v1.Group("/students")
		{
			me := studentGroup.Group("/me")
			me.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent))
			{
				// Profile
				me.GET("/profile", studentHandler.GetProfile)
				me.POST("/profile", studentHandler.UpsertProfile)

				// Documents
				me.POST("/documents", studentHandler.UploadDocument)
				me.GET("/documents", studentHandler.ListDocuments)
				me.POST("/documents/:id/default", studentHandler.SetDefaultDocument)
				me.DELETE("/documents/:id", studentHandler.DeleteDocument)

				// Applications & Interviews
				me.GET("/applications", appHandler.ListOwn)
				me.GET("/interviews", interviewHandler.ListStudent)
				me.POST("/interviews/:id/respond", interviewHandler.Respond)
			}
		}

		// Student Job Endpoints
		studentJobs := v1.Group("/jobs")
		studentJobs.Use(middleware.JWTAuthMiddleware(cfg, log), middleware.RequireRoles(models.RoleStudent))
		{
			studentJobs.POST("/:job_id/apply", appHandler.Apply)
		}

		// Employer & Admin Job Management Endpoints
		// Shared Authenticated Application Resources
		appsGroup := v1.Group("/applications")
		appsGroup.Use(middleware.JWTAuthMiddleware(cfg, log))
		{
			appsGroup.GET("/:id/answers", appHandler.ListAnswers)
			appsGroup.GET("/:id/history", appHandler.ListHistory)
		}
	}
}

// ============================================================================
// INITIALIZATION HELPERS
// ============================================================================

func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.RefreshToken{},
		&models.StudentProfile{},
		&models.StudentEducation{},
		&models.StudentSkill{},
		&models.StudentProject{},
		&models.StudentCertification{},
		&models.StudentDocument{},
		&models.Job{},
		&models.JobApplication{},
		&models.Interview{},
		&models.ApplicationStatusHistory{},
		&models.EmployerProfile{},
		&models.OrganizationVerification{},
	)
}

func seedAdminUser(db *gorm.DB, cfg *config.Config, log *logger.Logger) {
	adminEmail := getenv("ADMIN_EMAIL", "")
	adminPassword := getenv("ADMIN_PASSWORD", "")

	if adminEmail == "" || adminPassword == "" {
		return
	}

	userRepo := repositories.NewUserRepository(db)
	if _, err := userRepo.GetByEmail(adminEmail); err == nil {
		return // Admin user already exists
	}

	hash, err := services.NewAuthService(userRepo, cfg, log).HashPassword(adminPassword)
	if err != nil {
		log.Error("Failed to hash admin password: %v", err)
		return
	}

	admin := &models.User{
		Email:        adminEmail,
		PasswordHash: hash,
		FullName:     "Admin",
		Role:         models.RoleAdmin,
	}

	if err := userRepo.Create(admin); err != nil {
		log.Error("Failed to create admin user: %v", err)
		return
	}

	log.Info("Seeded admin user: %s", adminEmail)
}

func initRouter(cfg *config.Config) *gin.Engine {
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(CORSMiddleware())

	// Top-level health check endpoint
	router.GET("/health", healthCheckHandler)

	return router
}

func startServer(router *gin.Engine, cfg *config.Config, log *logger.Logger) {
	server := &http.Server{
		Addr:         ":" + cfg.App.Port,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	log.Info("Server starting on port %s", cfg.App.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("Server failed to start: %v", err)
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}

func healthCheckHandler(c *gin.Context) {
	responses.Success(c, http.StatusOK, "Health check passed", gin.H{
		"status":    "ok",
		"timestamp": time.Now(),
	})
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
