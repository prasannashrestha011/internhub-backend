package services

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

type EmployerProfileService struct {
	repo        *repositories.EmployerProfileRepository
	minioClient *minio.Client
	bucketName  string
}

const (
	maxOrganizationLogoSize = 5 * 1024 * 1024 // 5 MB
)

func NewEmployerProfileService(
	repo *repositories.EmployerProfileRepository,
	minioClient *minio.Client,
	bucketName string,
) *EmployerProfileService {
	return &EmployerProfileService{
		repo:        repo,
		minioClient: minioClient,
		bucketName:  bucketName,
	}
}

// CreateOrUpdateProfile creates a new employer profile
// or updates the existing profile for the user.
func (s *EmployerProfileService) CreateOrUpdateProfile(
	ctx context.Context,
	userID uuid.UUID,
	profile *models.EmployerProfile,
) (*models.EmployerProfile, error) {
	if userID == uuid.Nil {
		return nil, errors.New("invalid user id")
	}

	if profile == nil {
		return nil, errors.New("employer profile is required")
	}

	profile.OrganizationName = strings.TrimSpace(profile.OrganizationName)
	profile.Designation = strings.TrimSpace(profile.Designation)
	profile.OrganizationWebsite = strings.TrimSpace(profile.OrganizationWebsite)
	profile.OrganizationAddress = strings.TrimSpace(profile.OrganizationAddress)
	profile.OrganizationAbout = strings.TrimSpace(profile.OrganizationAbout)
	profile.Industry = strings.TrimSpace(profile.Industry)
	profile.OrganizationSize = strings.TrimSpace(profile.OrganizationSize)

	if profile.OrganizationName == "" {
		return nil, errors.New("organization name is required")
	}

	// Do not trust user_id coming from request body.
	// Always use authenticated user's ID.
	profile.UserID = userID

	if profile.ID == uuid.Nil {
		profile.ID = uuid.New()
	}

	if err := s.repo.Upsert(ctx, profile); err != nil {
		return nil, err
	}

	return s.repo.GetByUserID(ctx, userID)
}

// GetByUserID returns an employer profile by user ID.
func (s *EmployerProfileService) GetByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*models.EmployerProfile, error) {
	if userID == uuid.Nil {
		return nil, errors.New("invalid user id")
	}

	user, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	url, err := s.GetLogoURL(ctx, user.OrganizationLogo)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization logo URL: %w", err)
	}
	user.OrganizationLogo = url
	return user, nil
}

// GetByID returns an employer profile by profile ID.
func (s *EmployerProfileService) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*models.EmployerProfile, error) {
	if id == uuid.Nil {
		return nil, errors.New("invalid employer profile id")
	}

	return s.repo.GetByID(ctx, id)
}

// DeleteByUserID deletes an employer profile.
func (s *EmployerProfileService) DeleteByUserID(
	ctx context.Context,
	userID uuid.UUID,
) error {
	if userID == uuid.Nil {
		return errors.New("invalid user id")
	}

	return s.repo.DeleteByUserID(ctx, userID)
}

func (s *EmployerProfileService) UploadOrganizationLogo(
	ctx context.Context,
	userID uuid.UUID,
	fileHeader *multipart.FileHeader,
) (string, error) {

	if userID == uuid.Nil {
		return "", errors.New("invalid user id")
	}

	if fileHeader == nil {
		return "", errors.New("organization logo is required")
	}

	if fileHeader.Size <= 0 {
		return "", errors.New("uploaded file is empty")
	}

	if fileHeader.Size > maxOrganizationLogoSize {
		return "", errors.New("organization logo must be smaller than 5MB")
	}

	// Make sure employer profile exists.
	profile, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("failed to get employer profile: %w", err)
	}

	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open uploaded file: %w", err)
	}
	defer file.Close()

	// Read first 512 bytes for MIME detection.
	buffer := make([]byte, 512)

	n, err := file.Read(buffer)
	if err != nil && n == 0 {
		return "", fmt.Errorf("failed to read uploaded file: %w", err)
	}

	contentType := http.DetectContentType(buffer[:n])

	// Only allow actual image formats that we support.
	extension, err := getImageExtension(contentType)
	if err != nil {
		return "", err
	}

	// Reset file pointer after MIME detection.
	if _, err := file.Seek(0, 0); err != nil {
		return "", fmt.Errorf("failed to reset uploaded file: %w", err)
	}

	objectName := fmt.Sprintf(
		"organization-logos/%s/%s%s",
		userID.String(),
		uuid.New().String(),
		extension,
	)

	_, err = s.minioClient.PutObject(
		ctx,
		s.bucketName,
		objectName,
		file,
		fileHeader.Size,
		minio.PutObjectOptions{
			ContentType: contentType,
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to upload organization logo: %w", err)
	}

	oldLogo := profile.OrganizationLogo

	// Store only MinIO object key in DB.
	err = s.repo.UpdateOrganizationLogo(
		ctx,
		userID,
		objectName,
	)
	if err != nil {
		// DB update failed, so remove newly uploaded object.
		_ = s.minioClient.RemoveObject(
			ctx,
			s.bucketName,
			objectName,
			minio.RemoveObjectOptions{},
		)
	}

	// Remove previous logo after DB has successfully switched
	// to the new object.
	if oldLogo != "" && oldLogo != objectName {
		_ = s.minioClient.RemoveObject(
			ctx,
			s.bucketName,
			oldLogo,
			minio.RemoveObjectOptions{},
		)
	}
	presigned_url, err := s.GetLogoURL(ctx, objectName)
	if err != nil {
		return "", fmt.Errorf("failed to get presigned URL for organization logo: %w", err)
	}

	return presigned_url, nil

}
func (s *EmployerProfileService) GetLogoURL(
	ctx context.Context,
	objectKey string,
) (string, error) {

	if objectKey == "" {
		return "", nil
	}

	url, err := s.minioClient.PresignedGetObject(
		ctx,
		s.bucketName,
		objectKey,
		time.Hour,
		nil,
	)

	if err != nil {
		return "", err
	}

	return url.String(), nil
}

func getImageExtension(contentType string) (string, error) {
	switch strings.ToLower(contentType) {

	case "image/jpeg":
		return ".jpg", nil

	case "image/png":
		return ".png", nil

	case "image/webp":
		return ".webp", nil

	default:
		return "", errors.New(
			"unsupported image type; only JPEG, PNG and WEBP are allowed",
		)
	}
}
