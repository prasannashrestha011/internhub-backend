package repositories

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
)

func TestLegacyEmployerApprovalDoesNotRestrictPublicInternships(t *testing.T) {
	db := openApplicationRepositoryTestDB(t)
	repo := NewInternshipRepository(db)
	// Existing databases retain this retired column, but it must not affect access.
	if err := db.Exec("ALTER TABLE recruiter_profiles ADD COLUMN verification_status TEXT DEFAULT 'pending'").Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"approved", "pending", "rejected", "draft"} {
		t.Run(status, func(t *testing.T) {
			profile := models.RecruiterProfile{
				UserID: uuid.New(), OrganizationName: "TechKarkhana Nepal",
				OrganizationWebsite: "https://techkarkhana.example",
				OrganizationAddress: "Profile address",
			}
			if err := db.Create(&profile).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&profile).Update("verification_status", status).Error; err != nil {
				t.Fatal(err)
			}
			internship := createRepositoryTestInternship(t, db, profile.UserID, "Backend Intern")
			if err := repo.LoadPublicIssuer(context.Background(), internship); err != nil {
				t.Fatal(err)
			}
			issuer := internship.Issuer
			if issuer.OrganizationName != profile.OrganizationName || issuer.OrganizationWebsite != profile.OrganizationWebsite {
				t.Fatalf("public issuer fields not loaded: %+v", issuer)
			}
			if issuer.OrganizationAddress != "" || issuer.User != nil {
				t.Fatalf("extra issuer fields loaded: %+v", issuer)
			}
			results, total, err := repo.Search(context.Background(), InternshipSearchFilter{EmployerID: &profile.UserID, Status: enums.InternshipStatusPublished})
			if err != nil || total != 1 || len(results) != 1 || results[0].ID != internship.ID {
				t.Fatalf("legacy status %s hid a published internship: total=%d err=%v", status, total, err)
			}
		})
	}
}
