package storage

import (
	"context"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
)

func ConnectMinIO(cfg *config.MinIOConfig, logger *logger.Logger) (*minio.Client, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		logger.Error("Failed to connect to MinIO: %v", err)
		return nil, err
	}

	// Test connection and ensure buckets exist
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ensureBucketsExist(ctx, client, cfg, logger); err != nil {
		logger.Error("Failed to ensure buckets exist: %v", err)
		return nil, err
	}

	logger.Info("MinIO connected successfully")
	return client, nil
}

// Sign with the public host; changing the host after signing invalidates S3 signatures.
func NewURLSigner(cfg *config.MinIOConfig) (*minio.Client, error) {
	endpoint, secure := cfg.Endpoint, cfg.UseSSL
	if cfg.PublicURL != "" {
		u, err := url.Parse(cfg.PublicURL)
		if err != nil {
			return nil, err
		}
		endpoint, secure = u.Host, u.Scheme == "https"
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: region,
	})
}

func ensureBucketsExist(ctx context.Context, client *minio.Client, cfg *config.MinIOConfig, logger *logger.Logger) error {
	buckets := []string{
		cfg.ProfileBucket,
		cfg.StudentDocBucket,
	}

	for _, bucket := range buckets {
		exists, err := client.BucketExists(ctx, bucket)
		if err != nil {
			logger.Error("Failed to check bucket %s: %v", bucket, err)
			return err
		}

		if !exists {
			err = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
			if err != nil {
				logger.Error("Failed to create bucket %s: %v", bucket, err)
				return err
			}
			logger.Info("Created bucket: %s", bucket)
		}
	}

	return nil
}
