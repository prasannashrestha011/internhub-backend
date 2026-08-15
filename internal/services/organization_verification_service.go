package services

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"

	"github.com/prasanna/student-job-portal/backend/internal/enums"
	"github.com/prasanna/student-job-portal/backend/internal/models"
	"github.com/prasanna/student-job-portal/backend/internal/repositories"
)

var (
	ErrInvalidOrganizationVerification    = errors.New("invalid organization verification request")
	ErrOrganizationVerificationNotFound   = errors.New("organization verification not found")
	ErrInvalidVerificationState           = errors.New("invalid organization verification state")
	ErrOrganizationEmailAlreadyRegistered = errors.New("organization email is already registered for verification")
)

const maxOrganizationVerificationDocumentSize = 10 * 1024 * 1024 // 10 MB

type OrganizationVerificationService struct {
	repo                 *repositories.OrganizationVerificationRepository
	recruiterProfileRepo *repositories.RecruiterProfileRepository
	minioClient          *minio.Client
	bucketName           string
}

func NewOrganizationVerificationService(
	repo *repositories.OrganizationVerificationRepository,
	recruiterProfileRepo *repositories.RecruiterProfileRepository,
	minioClient *minio.Client,
	bucketName string,
) *OrganizationVerificationService {
	return &OrganizationVerificationService{
		repo: repo, recruiterProfileRepo: recruiterProfileRepo,
		minioClient: minioClient, bucketName: bucketName,
	}
}

// Submit creates or resubmits an employer's verification request.
func (s *OrganizationVerificationService) Submit(
	ctx context.Context,
	userID uuid.UUID,
	method enums.OrganizationVerificationMethod,
	organizationEmail string,
	documentType string,
) (*models.OrganizationVerification, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidOrganizationVerification)
	}
	organizationEmail = strings.TrimSpace(strings.ToLower(organizationEmail))
	address, err := mail.ParseAddress(organizationEmail)
	if err != nil || address.Address != organizationEmail {
		return nil, fmt.Errorf("%w: a valid organization email is required", ErrInvalidOrganizationVerification)
	}
	parts := strings.Split(address.Address, "@")
	if len(parts) != 2 || parts[1] == "" {
		return nil, fmt.Errorf("%w: a valid organization email is required", ErrInvalidOrganizationVerification)
	}
	if _, err := s.repo.GetByOrganizationEmail(ctx, organizationEmail); err == nil {
		return nil, ErrOrganizationEmailAlreadyRegistered
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("check organization email verification: %w", err)
	}

	recruiterProfile, err := s.recruiterProfileRepo.GetByUserID(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecruiterProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get recruiter profile: %w", err)
	}
	var oldDocumentObjectKey string
	if existing, err := s.repo.GetByRecruiterProfileID(ctx, recruiterProfile.ID); err == nil {
		oldDocumentObjectKey = existing.DocumentObjectKey
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("get existing organization verification: %w", err)
	}

	now := time.Now().UTC()
	verification := &models.OrganizationVerification{
		ID:                 uuid.New(),
		RecruiterProfileID: recruiterProfile.ID,
		Status:             enums.OrganizationVerificationPending,
		OrganizationEmail:  organizationEmail,
		EmailDomain:        address.Address[strings.Index(address.Address, "@")+1:],
		SubmittedAt:        &now,
		DocumentType:       strings.TrimSpace(documentType),
	}

	if err := s.repo.Upsert(ctx, verification); err != nil {
		return nil, fmt.Errorf("save organization verification: %w", err)
	}
	if err := s.recruiterProfileRepo.UpdateVerificationStatus(ctx, recruiterProfile.ID, string(enums.OrganizationVerificationPending)); err != nil {
		return nil, fmt.Errorf("update recruiter verification status: %w", err)
	}
	if oldDocumentObjectKey != "" {
		_ = s.minioClient.RemoveObject(ctx, s.bucketName, oldDocumentObjectKey, minio.RemoveObjectOptions{})
	}

	return s.GetMy(ctx, userID)
}

func (s *OrganizationVerificationService) GetMy(ctx context.Context, userID uuid.UUID) (*models.OrganizationVerification, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidOrganizationVerification)
	}
	recruiterProfile, err := s.recruiterProfileRepo.GetByUserID(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRecruiterProfileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get recruiter profile: %w", err)
	}
	verification, err := s.repo.GetByRecruiterProfileID(ctx, recruiterProfile.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrganizationVerificationNotFound
	}
	return verification, err
}

/*
UploadDocument uploads a verification document for an employer's verification request.
It can only be called if the verification method is "document" and the status is not "approved".
The document must be a PDF, JPEG, or PNG file and must be smaller than 10MB.
*/
func (s *OrganizationVerificationService) UploadDocument(ctx context.Context, userID uuid.UUID, fileHeader *multipart.FileHeader) (*models.OrganizationVerification, error) {
	if fileHeader == nil || fileHeader.Size <= 0 {
		return nil, fmt.Errorf("%w: verification document is required", ErrInvalidOrganizationVerification)
	}
	if fileHeader.Size > maxOrganizationVerificationDocumentSize {
		return nil, fmt.Errorf("%w: verification document must be smaller than 10MB", ErrInvalidOrganizationVerification)
	}

	verification, err := s.GetMy(ctx, userID)
	if err != nil {
		return nil, err
	}
	if verification.Status == enums.OrganizationVerificationApproved {
		return nil, fmt.Errorf("%w: document cannot be uploaded for this request", ErrInvalidVerificationState)
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, fmt.Errorf("open verification document: %w", err)
	}
	defer file.Close()

	buffer := make([]byte, 512)
	n, readErr := file.Read(buffer)
	if readErr != nil && n == 0 {
		return nil, fmt.Errorf("read verification document: %w", readErr)
	}
	contentType := http.DetectContentType(buffer[:n])
	extension, err := verificationDocumentExtension(contentType)
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("reset verification document: %w", err)
	}

	objectKey := fmt.Sprintf("organization-verifications/%s/%s%s", verification.RecruiterProfileID, uuid.New(), extension)
	if _, err := s.minioClient.PutObject(ctx, s.bucketName, objectKey, file, fileHeader.Size, minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return nil, fmt.Errorf("upload verification document: %w", err)
	}
	if err := s.repo.UpdateDocument(ctx, verification.ID, strings.TrimSpace(fileHeader.Filename), objectKey); err != nil {
		_ = s.minioClient.RemoveObject(ctx, s.bucketName, objectKey, minio.RemoveObjectOptions{})
		return nil, fmt.Errorf("save verification document: %w", err)
	}
	if verification.DocumentObjectKey != "" && verification.DocumentObjectKey != objectKey {
		_ = s.minioClient.RemoveObject(ctx, s.bucketName, verification.DocumentObjectKey, minio.RemoveObjectOptions{})
	}
	return s.GetMy(ctx, userID)
}

/*
review updates the status of an organization verification request.
It can only be called by an admin user.
The status must be either "approved" or "rejected".
If the status is "rejected", a rejection reason must be provided.
The function also updates the employer profile's verification status accordingly.
*/
func (s *OrganizationVerificationService) Review(ctx context.Context, id, reviewerID uuid.UUID, status enums.OrganizationVerificationStatus, rejectionReason, reviewNotes string) error {
	if id == uuid.Nil || reviewerID == uuid.Nil {
		return fmt.Errorf("%w: invalid verification or reviewer id", ErrInvalidOrganizationVerification)
	}
	if status != enums.OrganizationVerificationApproved && status != enums.OrganizationVerificationRejected {
		return fmt.Errorf("%w: review status must be approved or rejected", ErrInvalidOrganizationVerification)
	}
	rejectionReason, reviewNotes = strings.TrimSpace(rejectionReason), strings.TrimSpace(reviewNotes)
	if status == enums.OrganizationVerificationRejected && rejectionReason == "" {
		return fmt.Errorf("%w: rejection reason is required", ErrInvalidOrganizationVerification)
	}

	verification, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrOrganizationVerificationNotFound
	}
	if err != nil {
		return err
	}
	if verification.Status != enums.OrganizationVerificationPending {
		return fmt.Errorf("%w: only pending requests can be reviewed", ErrInvalidVerificationState)
	}

	now := time.Now().UTC()
	updates := map[string]interface{}{"status": status, "reviewed_by": reviewerID, "reviewed_at": now, "review_notes": reviewNotes, "rejection_reason": rejectionReason}
	if status == enums.OrganizationVerificationApproved {
		updates["verified_at"] = now
	}

	return s.repo.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		verificationRepo := repositories.NewOrganizationVerificationRepository(tx)
		profileRepo := repositories.NewRecruiterProfileRepository(tx)
		if err := verificationRepo.UpdateReview(ctx, id, updates); err != nil {
			return err
		}
		return profileRepo.UpdateVerificationStatus(ctx, verification.RecruiterProfileID, string(status))
	})
}

func (s *OrganizationVerificationService) GetByID(ctx context.Context, id uuid.UUID) (*models.OrganizationVerification, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: invalid verification id", ErrInvalidOrganizationVerification)
	}
	verification, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrOrganizationVerificationNotFound
	}
	return verification, err
}

/*
List retrieves a paginated list of organization verification requests filtered by status.
The status can be "pending", "approved", or "rejected". If no status is provided, all requests are returned.
The page and pageSize parameters control the pagination of the results.
*/
func (s *OrganizationVerificationService) List(ctx context.Context, filters repositories.VerificationSearchFilter) ([]models.OrganizationVerification, int64, error) {
	status := filters.Status
	page := filters.Page
	pageSize := filters.PageSize

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	items, err := s.repo.GetByStatus(ctx, status, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountByStatus(ctx, status)
	return items, total, err
}

/*
GetDocumentURL generates a pre-signed URL for accessing a verification document in the object storage.
*/
func (s *OrganizationVerificationService) GetDocumentURL(ctx context.Context, objectKey string) (string, error) {
	if objectKey == "" {
		return "", nil
	}
	url, err := s.minioClient.PresignedGetObject(ctx, s.bucketName, objectKey, time.Hour, nil)
	if err != nil {
		return "", fmt.Errorf("create document URL: %w", err)
	}
	return url.String(), nil
}

func verificationDocumentExtension(contentType string) (string, error) {
	switch strings.ToLower(contentType) {
	case "application/pdf":
		return ".pdf", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	default:
		return "", fmt.Errorf("%w: only PDF, JPEG, and PNG documents are allowed", ErrInvalidOrganizationVerification)
	}
}
