package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
	"gorm.io/gorm"
)

var (
	ErrInvalidStudentData     = errors.New("invalid student data")
	ErrStudentProfileNotFound = errors.New("student profile not found")
)

const maxResumeSize = 10 << 20

type StudentService struct {
	repo        *repositories.StudentRepository
	minioClient *minio.Client
	urlSigner   *minio.Client
	cfg         *config.Config
	log         *logger.Logger
}

func NewStudentService(repo *repositories.StudentRepository, storage, urlSigner *minio.Client, cfg *config.Config, log *logger.Logger) *StudentService {
	return &StudentService{repo: repo, minioClient: storage, urlSigner: urlSigner, cfg: cfg, log: log}
}

func (s *StudentService) GetProfile(userID uuid.UUID) (*models.StudentProfile, error) {
	profile, err := s.repo.GetByUserID(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrStudentProfileNotFound
	}
	if err != nil {
		return nil, err
	}
	return profile, s.setProfilePictureURL(context.Background(), profile)
}

func (s *StudentService) setProfilePictureURL(ctx context.Context, profile *models.StudentProfile) error {
	if profile.ProfilePictureKey == "" {
		return nil
	}
	if s.urlSigner == nil {
		return errors.New("profile picture storage unavailable")
	}
	url, err := s.urlSigner.PresignedGetObject(ctx, s.cfg.MinIO.ProfileBucket, profile.ProfilePictureKey, time.Hour, nil)
	if err != nil {
		return err
	}
	profile.ProfilePicture = url.String()
	return nil
}

func (s *StudentService) GetApplicationStats(ctx context.Context, profileID uuid.UUID) (*repositories.StudentApplicationStats, error) {
	return s.repo.GetApplicationStats(ctx, profileID)
}

func (s *StudentService) CreateOrUpdateProfile(ctx context.Context, profile *models.StudentProfile) error {
	if profile == nil || profile.UserID == uuid.Nil {
		return ErrInvalidStudentData
	}
	profile.FullName = strings.TrimSpace(profile.FullName)
	if profile.FullName == "" || len(profile.FullName) > 255 || len(profile.Phone) > 50 || len(profile.Location) > 255 || len(profile.Bio) > 5000 || len(profile.CollegeName) > 255 || len(profile.Degree) > 255 || len(profile.FacultyOrMajor) > 255 || len(profile.CurrentSemester) > 64 || profile.GraduationYear < 0 || profile.GraduationYear > 9999 {
		return fmt.Errorf("%w: name is required and profile fields must fit their limits", ErrInvalidStudentData)
	}
	for _, link := range []string{profile.LinkedinURL, profile.GithubURL, profile.PortfolioURL} {
		if len(link) > 255 || (link != "" && !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://")) {
			return fmt.Errorf("%w: links must use http or https and be at most 255 characters", ErrInvalidStudentData)
		}
	}
	profile.ID, profile.CreatedAt, profile.DeletedAt = uuid.Nil, time.Time{}, gorm.DeletedAt{}
	profile.ProfilePictureKey, profile.ProfilePicture = "", ""
	if err := s.repo.CreateOrUpdateProfile(ctx, profile); err != nil {
		return err
	}
	return s.setProfilePictureURL(ctx, profile)
}

func (s *StudentService) UploadProfilePicture(ctx context.Context, userID uuid.UUID, file *multipart.FileHeader) (*models.StudentProfile, error) {
	profile, err := s.GetProfile(userID)
	if err != nil {
		return nil, err
	}
	data, contentType, extension, err := readProfilePicture(file)
	if err != nil {
		return nil, err
	}
	if s.minioClient == nil {
		return nil, errors.New("profile picture storage unavailable")
	}
	key := fmt.Sprintf("student-pictures/%s/%s%s", userID, uuid.New(), extension)
	bucket := s.cfg.MinIO.ProfileBucket
	if _, err := s.minioClient.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return nil, err
	}
	oldKey, err := s.repo.UpdateProfilePicture(ctx, userID, key)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err != nil {
		if cleanupErr := s.minioClient.RemoveObject(cleanupCtx, bucket, key, minio.RemoveObjectOptions{}); cleanupErr != nil {
			s.log.Error("clean up failed profile picture upload: %v", cleanupErr)
		}
		return nil, err
	}
	if oldKey != "" {
		if err := s.minioClient.RemoveObject(cleanupCtx, bucket, oldKey, minio.RemoveObjectOptions{}); err != nil {
			s.log.Error("clean up replaced profile picture: %v", err)
		}
	}
	profile.ProfilePictureKey = key
	return profile, s.setProfilePictureURL(ctx, profile)
}

func (s *StudentService) GetResume(ctx context.Context, userID uuid.UUID) (*models.StudentDocument, error) {
	profile, err := s.GetProfile(userID)
	if err != nil {
		return nil, err
	}
	resume, err := s.repo.GetResume(ctx, profile.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if s.urlSigner == nil {
		return nil, errors.New("CV storage unavailable")
	}
	link, err := s.urlSigner.PresignedGetObject(ctx, s.cfg.MinIO.StudentDocBucket, resume.ObjectKey, 5*time.Minute, nil)
	if err != nil {
		return nil, err
	}
	resume.URL = link.String()
	return resume, nil
}

func (s *StudentService) UploadResume(ctx context.Context, userID uuid.UUID, file *multipart.FileHeader) (*models.StudentDocument, error) {
	profile, err := s.GetProfile(userID)
	if err != nil {
		return nil, err
	}
	if file == nil || file.Size <= 0 || file.Size > maxResumeSize {
		return nil, fmt.Errorf("%w: upload a PDF CV of at most 10 MiB", ErrInvalidStudentData)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxResumeSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResumeSize || http.DetectContentType(data) != "application/pdf" {
		return nil, fmt.Errorf("%w: upload a PDF CV of at most 10 MiB", ErrInvalidStudentData)
	}
	if s.minioClient == nil {
		return nil, errors.New("CV storage unavailable")
	}
	resume := &models.StudentDocument{UserID: userID, ProfileID: profile.ID, FileName: filepath.Base(file.Filename), MimeType: "application/pdf", Size: int64(len(data)), ObjectKey: fmt.Sprintf("students/%s/%s.pdf", userID, uuid.New())}
	bucket := s.cfg.MinIO.StudentDocBucket
	if _, err := s.minioClient.PutObject(ctx, bucket, resume.ObjectKey, bytes.NewReader(data), resume.Size, minio.PutObjectOptions{ContentType: "application/pdf"}); err != nil {
		return nil, err
	}
	oldKey, err := s.repo.SaveResume(ctx, resume)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err != nil {
		if cleanupErr := s.minioClient.RemoveObject(cleanupCtx, bucket, resume.ObjectKey, minio.RemoveObjectOptions{}); cleanupErr != nil {
			s.log.Error("clean up failed CV upload: %v", cleanupErr)
		}
		return nil, err
	}
	if oldKey != "" {
		if err := s.minioClient.RemoveObject(cleanupCtx, bucket, oldKey, minio.RemoveObjectOptions{}); err != nil {
			s.log.Error("clean up replaced CV: %v", err)
		}
	}
	return resume, nil
}

func (s *StudentService) DeleteResume(ctx context.Context, userID uuid.UUID) error {
	profile, err := s.GetProfile(userID)
	if err != nil {
		return err
	}
	if s.minioClient == nil {
		return errors.New("CV storage unavailable")
	}
	documents, err := s.repo.DeleteResume(ctx, profile.ID, userID)
	if err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	// shortcut: storage cleanup is best effort, add retries if storage failures persist.
	for _, document := range documents {
		if err := s.minioClient.RemoveObject(cleanupCtx, s.cfg.MinIO.StudentDocBucket, document.ObjectKey, minio.RemoveObjectOptions{}); err != nil {
			s.log.Error("clean up deleted CV: %v", err)
		}
	}
	return nil
}
