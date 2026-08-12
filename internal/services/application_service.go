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

// ApplicationService handles job application flows
type ApplicationService struct {
	Repo        *repositories.JobApplicationRepository
	HistoryRepo *repositories.ApplicationStatusHistoryRepository
	MinioClient *minio.Client
	Cfg         *config.Config
	Log         *logger.Logger
}

func NewApplicationService(repo *repositories.JobApplicationRepository, historyRepo *repositories.ApplicationStatusHistoryRepository, minioClient *minio.Client, cfg *config.Config, l *logger.Logger) *ApplicationService {
	return &ApplicationService{Repo: repo, HistoryRepo: historyRepo, MinioClient: minioClient, Cfg: cfg, Log: l}
}

// Apply uploads resume to MinIO and creates an application record
func (s *ApplicationService) Apply(ctx context.Context, jobID, studentID uuid.UUID, coverNote string, resume *multipart.FileHeader, answers []models.ApplicationAnswer, maxSize int64, allowed map[string]bool, ansRepo *repositories.ApplicationAnswerRepository, statusRepo *repositories.ApplicationStatusRepository) (*models.JobApplication, error) {
	var resumeKey string
	if resume != nil {
		if resume.Size <= 0 || resume.Size > maxSize {
			return nil, fmt.Errorf("resume size exceeds limit")
		}
		f, err := resume.Open()
		if err != nil {
			return nil, err
		}
		defer f.Close()
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		contentType := resume.Header.Get("Content-Type")
		if contentType == "" {
			contentType = http.DetectContentType(buf[:n])
		}
		if !allowed[contentType] {
			return nil, fmt.Errorf("resume content type not allowed: %s", contentType)
		}
		var reader io.Reader
		if seeker, ok := f.(io.Seeker); ok {
			_, _ = seeker.Seek(0, io.SeekStart)
			reader = f
		} else {
			reader = io.MultiReader(bytes.NewReader(buf[:n]), f)
		}
		ext := filepath.Ext(resume.Filename)
		resumeKey = fmt.Sprintf("applications/%s/resume_%s%s", studentID.String(), uuid.New().String(), ext)
		bucket := s.Cfg.MinIO.CompanyDocBucket
		info, err := s.MinioClient.PutObject(ctx, bucket, resumeKey, reader, resume.Size, minio.PutObjectOptions{ContentType: contentType})
		if err != nil {
			s.Log.Error("failed to upload resume: %v", err)
			return nil, err
		}
		_ = info
	}

	app := &models.JobApplication{
		JobID:     jobID,
		StudentID: studentID,
		CoverNote: coverNote,
		ResumeKey: resumeKey,
		Status:    "applied",
	}
	if err := s.Repo.Create(app); err != nil {
		return nil, err
	}
	// persist answers if provided
	for i := range answers {
		answers[i].ApplicationID = app.ID
		if err := ansRepo.Create(&answers[i]); err != nil {
			return nil, err
		}
	}
	// record initial status history
	history := &models.ApplicationStatusHistory{
		ApplicationID: app.ID,
		FromStatus:    "",
		ToStatus:      app.Status,
		ChangedBy:     studentID.String(),
	}
	if err := statusRepo.Create(history); err != nil {
		return nil, err
	}
	return app, nil
}

// UpdateStatus updates application status and persists (and records history)
func (s *ApplicationService) UpdateStatus(id uuid.UUID, status string) (*models.JobApplication, error) {
	app, err := s.Repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	old := app.Status
	// if no change, return as-is
	if old == status {
		return app, nil
	}
	app.Status = status
	if err := s.Repo.Update(app); err != nil {
		return nil, err
	}
	// record history if repository provided
	if s.HistoryRepo != nil {
		h := &models.ApplicationStatusHistory{
			ApplicationID: app.ID,
			FromStatus:    old,
			ToStatus:      status,
			ChangedBy:     "",
			Reason:        "",
		}
		if err := s.HistoryRepo.Create(h); err != nil {
			// log but don't fail the status update because of history persistence issues
			s.Log.Error("failed to record application status history: %v", err)
		}
	}
	return app, nil
}
