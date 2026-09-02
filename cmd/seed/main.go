package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/database"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/seeder"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(fmt.Sprintf("load configuration: %v", err))
	}
	if strings.EqualFold(cfg.App.Env, "production") {
		panic("refusing to seed when APP_ENV=production")
	}

	log := logger.New(cfg.App.Env)
	db, err := database.Connect(&cfg.Database, log)
	if err != nil {
		panic(fmt.Sprintf("connect database: %v", err))
	}
	defer func() { _ = database.Close(db) }()

	if err := database.AutoMigrate(db); err != nil {
		panic(fmt.Sprintf("migrate database: %v", err))
	}

	password := os.Getenv("SEED_PASSWORD")
	usingDefaultPassword := password == ""
	summary, err := seeder.Run(db, password)
	if err != nil {
		panic(err)
	}

	fmt.Printf(
		"Seed complete: %d users, %d student profiles, %d employer profiles, %d verifications, %d internships, %d applications.\n",
		summary.Users,
		summary.StudentProfiles,
		summary.RecruiterProfiles,
		summary.OrganizationVerifications,
		summary.Internships,
		summary.Applications,
	)
	if usingDefaultPassword {
		fmt.Printf("Development login password: %s (override with SEED_PASSWORD).\n", seeder.DefaultPassword)
	} else {
		fmt.Println("Development login password was read from SEED_PASSWORD.")
	}
}
