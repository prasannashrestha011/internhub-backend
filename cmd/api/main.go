package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"github.com/prasanna/student-job-portal/backend/internal/routes"
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
	routes.Register(router, db, cfg, log, minioClient)

	// 6. Start Server
	startServer(router, cfg, log)
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
		&models.Internship{},
		&models.JobApplication{},
		&models.Interview{},
		&models.ApplicationStatusHistory{},
		&models.RecruiterProfile{},
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

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
