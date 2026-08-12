package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"

	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

// StudentService provides student-related operations that use repositories and MinIO.
type StudentService struct {
	repo        *repositories.StudentRepository
	minioClient *minio.Client
	cfg         *config.Config
	l           *logger.Logger
}

// NewStudentService constructs a new StudentService.
func NewStudentService(repo *repositories.StudentRepository, minioClient *minio.Client, cfg *config.Config, l *logger.Logger) *StudentService {
	return &StudentService{repo: repo, minioClient: minioClient, cfg: cfg, l: l}
}

// CalculateProfileCompletion returns a simple profile completion percentage based
// on the fraction of non-empty key fields in the profile.
func (s *StudentService) CalculateProfileCompletion(profile *models.StudentProfile) int {
	if profile == nil {
		return 0
	}

	// Choose a set of key fields to consider for completion.
	fields := []string{
		profile.FullName,
		profile.Phone,
		profile.Location,
		profile.Bio,
		profile.CollegeName,
		profile.Degree,
		profile.FacultyOrMajor,
		profile.CurrentSemester,
		profile.PreferredJobCategories,
		profile.PreferredLocations,
		profile.PreferredWorkMode,
		profile.Availability,
		profile.ExpectedSalary,
		profile.LinkedinURL,
		profile.GithubURL,
		profile.PortfolioURL,
		profile.ProfileImageKey,
	}

	total := len(fields) + 1 // include GraduationYear as a numeric field
	present := 0
	for _, f := range fields {
		if f != "" {
			present++
		}
	}
	if profile.GraduationYear > 0 {
		present++
	}

	if total == 0 {
		return 0
	}

	percent := int(float64(present) / float64(total) * 100)
	return percent
}

// UploadDocument uploads the provided multipart file header to MinIO under the configured
// student documents bucket. Returns the object key, size in bytes, detected mime type and error.
func (s *StudentService) UploadDocument(userID, profileID uuid.UUID, fileHeader *multipart.FileHeader) (string, int64, string, error) {
	if fileHeader == nil {
		return "", 0, "", fmt.Errorf("file header is nil")
	}

	f, err := fileHeader.Open()
	if err != nil {
		return "", 0, "", fmt.Errorf("opening uploaded file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Read up to 512 bytes for content type detection
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(buf[:n])
	}

	var reader io.Reader
	// Try to rewind the file if it supports seeking
	if seeker, ok := f.(io.Seeker); ok {
		_, _ = seeker.Seek(0, io.SeekStart)
		reader = f
	} else {
		// Non-seekable: prepend the portion we already read
		reader = io.MultiReader(bytes.NewReader(buf[:n]), f)
	}

	// Build object key: students/{userID}/{profileID}/{uuid}{ext}
	ext := filepath.Ext(fileHeader.Filename)
	objectKey := fmt.Sprintf("students/%s/%s/%s%s", userID.String(), profileID.String(), uuid.New().String(), ext)

	ctx := context.Background()
	bucket := s.cfg.MinIO.StudentDocBucket
	putOpts := minio.PutObjectOptions{ContentType: contentType}

	info, err := s.minioClient.PutObject(ctx, bucket, objectKey, reader, fileHeader.Size, putOpts)
	if err != nil {
		s.l.Error("failed to upload object to minio", err)
		return "", 0, "", fmt.Errorf("put object: %w", err)
	}

	return objectKey, info.Size, contentType, nil
}

// DeleteDocument removes the object identified by objectKey from the student documents bucket.
func (s *StudentService) DeleteDocument(objectKey string) error {
	if objectKey == "" {
		return fmt.Errorf("object key is empty")
	}
	ctx := context.Background()
	bucket := s.cfg.MinIO.StudentDocBucket
	err := s.minioClient.RemoveObject(ctx, bucket, objectKey, minio.RemoveObjectOptions{})
	if err != nil {
		s.l.Error("failed to remove object from minio", err)
		return err
	}
	return nil
}
