package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
	urlSigner, err := storage.NewURLSigner(&cfg.MinIO)
	if err != nil {
		log.Fatal("Failed to configure storage URL signing: %v", err)
	}

	// 4. Initialize Engine & Register Routes
	router, err := initRouter(cfg)
	if err != nil {
		log.Fatal("Failed to configure HTTP router: %v", err)
	}
	routes.Register(router, db, cfg, log, minioClient, urlSigner)

	// 5. Start Server
	if err := startServer(router, cfg, log); err != nil {
		log.Fatal("HTTP server failed: %v", err)
	}
}

// ============================================================================
// INITIALIZATION HELPERS
// ============================================================================

func initRouter(cfg *config.Config) (*gin.Engine, error) {
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	if err := router.SetTrustedProxies(cfg.App.TrustedProxies); err != nil {
		return nil, err
	}
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	router.Use(CORSMiddleware(cfg.App.AllowedOrigins))
	router.Use(func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12<<20)
		c.Next()
	})

	return router, nil
}

func startServer(router *gin.Engine, cfg *config.Config, log *logger.Logger) error {
	server := &http.Server{
		Addr:              ":" + cfg.App.Port,
		Handler:           router,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Info("Server starting on port %s", cfg.App.Port)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, server)
}

func serve(ctx context.Context, server *http.Server) error {
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func CORSMiddleware(origins []string) gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length", "Retry-After"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	})
}
