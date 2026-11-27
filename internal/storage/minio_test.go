package storage

import (
	"context"
	"testing"
	"time"

	"github.com/prasanna/student-job-portal/backend/internal/config"
)

func TestPublicURLSigningDoesNotContactPublicEndpoint(t *testing.T) {
	cfg := &config.MinIOConfig{Endpoint: "minio:9000", PublicURL: "https://files.invalid", AccessKey: "key", SecretKey: "secret", Region: "us-east-1"}
	client, err := NewURLSigner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	u, err := client.PresignedGetObject(context.Background(), "documents", "student/cv.pdf", time.Minute, nil)
	if err != nil || u.Host != "files.invalid" || u.Scheme != "https" || u.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("invalid public signed URL: %v %v", u, err)
	}
}
