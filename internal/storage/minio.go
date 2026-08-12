package storage

import (
	"context"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/prasanna/student-job-portal/backend/internal/config"
	"github.com/prasanna/student-job-portal/backend/internal/logger"
)

func ConnectMinIO(cfg *config.MinIOConfig, logger *logger.Logger) (*minio.Client, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		logger.Error("Failed to connect to MinIO: %v", err)
		return nil, err
	}

	// Test connection and ensure buckets exist
	ctx := context.Background()
	if err := ensureBucketsExist(ctx, client, cfg, logger); err != nil {
		logger.Error("Failed to ensure buckets exist: %v", err)
		return nil, err
	}

	logger.Info("MinIO connected successfully")
	return client, nil
}

func ensureBucketsExist(ctx context.Context, client *minio.Client, cfg *config.MinIOConfig, logger *logger.Logger) error {
	buckets := []string{
		cfg.ProfileBucket,
		cfg.StudentDocBucket,
		cfg.CompanyLogoBucket,
		cfg.CompanyDocBucket,
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
