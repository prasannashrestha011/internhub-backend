package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

var (
	ErrInvalidRecruiterProfileData = errors.New("invalid recruiter profile data")
	ErrRecruiterProfileNotFound    = errors.New("recruiter profile not found")
)

type RecruiterProfileService struct {
	repo        *repositories.RecruiterProfileRepository
	minioClient *minio.Client
	bucketName  string
}

func NewRecruiterProfileService(
	repo *repositories.RecruiterProfileRepository,
	minioClient *minio.Client,
	bucketName string,
) *RecruiterProfileService {
	return &RecruiterProfileService{
		repo:        repo,
		minioClient: minioClient,
		bucketName:  bucketName,
	}
}

// CreateOrUpdateProfile creates a new recruiter profile
// or updates the existing profile for the user.
func (s *RecruiterProfileService) CreateOrUpdateProfile(
	ctx context.Context,
	userID uuid.UUID,
	profile *models.RecruiterProfile,
) (*models.RecruiterProfile, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidRecruiterProfileData)
	}

	if profile == nil {
		return nil, fmt.Errorf("%w: recruiter profile is required", ErrInvalidRecruiterProfileData)
	}

	profile.OrganizationName = strings.TrimSpace(profile.OrganizationName)
	profile.Designation = strings.TrimSpace(profile.Designation)
	profile.OrganizationWebsite = strings.TrimSpace(profile.OrganizationWebsite)
	profile.OrganizationAddress = strings.TrimSpace(profile.OrganizationAddress)
	profile.OrganizationAbout = strings.TrimSpace(profile.OrganizationAbout)
	profile.Industry = strings.TrimSpace(profile.Industry)
	profile.OrganizationSize = strings.TrimSpace(profile.OrganizationSize)

	if profile.OrganizationName == "" {
		return nil, fmt.Errorf("%w: organization name is required", ErrInvalidRecruiterProfileData)
	}

	// Do not trust user_id coming from request body.
	// Always use authenticated user's ID.
	profile.UserID = userID
	profile.User = nil
	profile.OrganizationLogo = ""

	profile.ID = uuid.New()

	if err := s.repo.Upsert(ctx, profile); err != nil {
		return nil, err
	}

	return s.GetByUserID(ctx, userID)
}

// GetByUserID returns an recruiter profile by user ID.
func (s *RecruiterProfileService) GetByUserID(
	ctx context.Context,
	userID uuid.UUID,
) (*models.RecruiterProfile, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidRecruiterProfileData)
	}

	user, err := s.repo.GetByUserID(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecruiterProfileNotFound
	}
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

// GetByID returns an recruiter profile by profile ID.
func (s *RecruiterProfileService) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*models.RecruiterProfile, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid recruiter profile id", ErrInvalidRecruiterProfileData)
	}

	profile, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecruiterProfileNotFound
	}
	return profile, err
}

// DeleteByUserID deletes an recruiter profile.
func (s *RecruiterProfileService) DeleteByUserID(
	ctx context.Context,
	userID uuid.UUID,
) error {
	if userID == uuid.Nil {
		return fmt.Errorf("%w: invalid user id", ErrInvalidRecruiterProfileData)
	}
	if _, err := s.GetByUserID(ctx, userID); err != nil {
		return err
	}
	return s.repo.DeleteByUserID(ctx, userID)
}

func (s *RecruiterProfileService) UploadOrganizationLogo(
	ctx context.Context,
	userID uuid.UUID,
	fileHeader *multipart.FileHeader,
) (string, error) {

	if userID == uuid.Nil {
		return "", fmt.Errorf("%w: invalid user id", ErrInvalidRecruiterProfileData)
	}

	data, contentType, extension, err := readProfilePicture(fileHeader)
	if err != nil {
		return "", err
	}
	if s.minioClient == nil {
		return "", errors.New("profile picture storage unavailable")
	}

	// Make sure recruiter profile exists.
	profile, err := s.repo.GetByUserID(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrRecruiterProfileNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get recruiter profile: %w", err)
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
		bytes.NewReader(data),
		int64(len(data)),
		minio.PutObjectOptions{
			ContentType: contentType,
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to upload organization logo: %w", err)
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
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
			cleanupCtx,
			s.bucketName,
			objectName,
			minio.RemoveObjectOptions{},
		)
		return "", fmt.Errorf("update organization logo: %w", err)
	}

	// Remove previous logo after DB has successfully switched
	// to the new object.
	if oldLogo != "" && oldLogo != objectName {
		_ = s.minioClient.RemoveObject(
			cleanupCtx,
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
func (s *RecruiterProfileService) GetLogoURL(
	ctx context.Context,
	objectKey string,
) (string, error) {

	if objectKey == "" {
		return "", nil
	}

	if s.minioClient == nil {
		return "", errors.New("profile picture storage unavailable")
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

// listingLogoURL keeps listings available if the optional logo cannot be signed.
func (s *RecruiterProfileService) listingLogoURL(ctx context.Context, objectKey string) string {
	url, err := s.GetLogoURL(ctx, objectKey)
	if err != nil {
		slog.WarnContext(ctx, "Could not sign company logo", "error", err)
	}
	return url
}
