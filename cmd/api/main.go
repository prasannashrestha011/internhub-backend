package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/routes"
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

	if err := database.AutoMigrate(db); err != nil {
		log.Fatal("AutoMigrate failed: %v", err)
	}

	// 3. Connect MinIO Storage
	minioClient, err := storage.ConnectMinIO(&cfg.MinIO, log)
	if err != nil {
		log.Fatal("Failed to connect to MinIO: %v", err)
	}

	// 4. Initialize Engine & Register Routes
	router := initRouter(cfg)
	routes.Register(router, db, cfg, log, minioClient)

	// 5. Start Server
	startServer(router, cfg, log)
}

// ============================================================================
// INITIALIZATION HELPERS
// ============================================================================

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
		ExposeHeaders:    []string{"Content-Length", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
