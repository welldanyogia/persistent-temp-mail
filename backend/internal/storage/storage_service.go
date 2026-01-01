package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/welldanyogia/persistent-temp-mail/backend/internal/config"
)

// StorageService handles S3/MinIO operations for attachment storage
type StorageService struct {
	client             *s3.Client
	presignClient      *s3.PresignClient
	bucket             string
	presignedURLExpiry time.Duration
	largeFileThreshold int64
}

// NewStorageService creates a new storage service with S3/MinIO client
func NewStorageService(cfg *config.StorageConfig) (*StorageService, error) {
	// Build endpoint URL - handle case where endpoint already includes protocol
	var endpointURL string
	if strings.HasPrefix(cfg.Endpoint, "http://") || strings.HasPrefix(cfg.Endpoint, "https://") {
		endpointURL = cfg.Endpoint
	} else {
		protocol := "http"
		if cfg.UseSSL {
			protocol = "https"
		}
		endpointURL = protocol + "://" + cfg.Endpoint
	}

	// Create S3 client with custom endpoint for MinIO compatibility
	client := s3.New(s3.Options{
		Region: cfg.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		),
		BaseEndpoint: aws.String(endpointURL),
		UsePathStyle: true, // Required for MinIO
	})

	// Create presign client for generating pre-signed URLs
	presignClient := s3.NewPresignClient(client)

	// Set default values if not configured
	presignedURLExpiry := cfg.PresignedURLExpiry
	if presignedURLExpiry == 0 {
		presignedURLExpiry = 15 * time.Minute // Default: 15 minutes
	}

	largeFileThreshold := cfg.LargeFileThreshold
	if largeFileThreshold == 0 {
		largeFileThreshold = 10 * 1024 * 1024 // Default: 10 MB
	}

	return &StorageService{
		client:             client,
		presignClient:      presignClient,
		bucket:             cfg.Bucket,
		presignedURLExpiry: presignedURLExpiry,
		largeFileThreshold: largeFileThreshold,
	}, nil
}

// DeleteByKeys deletes multiple objects from S3 by their storage keys
// Returns the count of deleted objects and total size freed in bytes
// Requirements: 5.2, 5.3 (Delete attachments from storage)
func (s *StorageService) DeleteByKeys(ctx context.Context, keys []string) (int, int64, error) {
	if len(keys) == 0 {
		return 0, 0, nil
	}

	// First, get the sizes of all objects to calculate total size freed
	var totalSize int64
	for _, key := range keys {
		headOutput, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			// Object might not exist, continue with deletion
			continue
		}
		if headOutput.ContentLength != nil {
			totalSize += *headOutput.ContentLength
		}
	}

	// Build delete objects input
	objectIdentifiers := make([]types.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objectIdentifiers[i] = types.ObjectIdentifier{
			Key: aws.String(key),
		}
	}

	// Delete objects in batch (S3 supports up to 1000 objects per request)
	deleteCount := 0
	batchSize := 1000

	for i := 0; i < len(objectIdentifiers); i += batchSize {
		end := i + batchSize
		if end > len(objectIdentifiers) {
			end = len(objectIdentifiers)
		}

		batch := objectIdentifiers[i:end]
		output, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{
				Objects: batch,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return deleteCount, totalSize, fmt.Errorf("failed to delete objects: %w", err)
		}

		// Count successful deletions
		deleteCount += len(batch) - len(output.Errors)
	}

	return deleteCount, totalSize, nil
}

// DeleteByAliasID deletes all attachments associated with an alias
// This method requires storage keys to be provided externally (from repository)
// Returns the count of deleted objects and total size freed in bytes
// Requirements: 5.2, 5.3 (Delete attachments from storage)
func (s *StorageService) DeleteByAliasID(ctx context.Context, aliasID string) (int, int64, error) {
	// This method is a convenience wrapper that lists objects by prefix
	// The prefix pattern is: attachments/{aliasID}/
	prefix := fmt.Sprintf("attachments/%s/", aliasID)

	// List all objects with the prefix
	var keys []string
	var totalSize int64

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
			if obj.Size != nil {
				totalSize += *obj.Size
			}
		}
	}

	if len(keys) == 0 {
		return 0, 0, nil
	}

	// Delete all found objects
	deleteCount, _, err := s.DeleteByKeys(ctx, keys)
	if err != nil {
		return deleteCount, totalSize, err
	}

	return deleteCount, totalSize, nil
}

// DeleteObject deletes a single object from S3
func (s *StorageService) DeleteObject(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object %s: %w", key, err)
	}
	return nil
}

// GetClient returns the underlying S3 client for advanced operations
func (s *StorageService) GetClient() *s3.Client {
	return s.client
}

// GetBucket returns the configured bucket name
func (s *StorageService) GetBucket() string {
	return s.bucket
}

// GetPresignedURL generates a pre-signed URL for downloading an object
// The URL expires after the configured duration (default: 15 minutes)
// Requirements: 3.2 (Generate pre-signed URL with 15-minute expiration)
func (s *StorageService) GetPresignedURL(ctx context.Context, key string) (string, time.Duration, error) {
	return s.GetPresignedURLWithExpiry(ctx, key, s.presignedURLExpiry)
}

// GetPresignedURLWithExpiry generates a pre-signed URL with custom expiration
// Requirements: 3.2 (Generate pre-signed URL with configurable expiration)
func (s *StorageService) GetPresignedURLWithExpiry(ctx context.Context, key string, expiry time.Duration) (string, time.Duration, error) {
	presignedReq, err := s.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", 0, fmt.Errorf("failed to generate pre-signed URL: %w", err)
	}

	return presignedReq.URL, expiry, nil
}

// GetPresignedURLExpiry returns the configured pre-signed URL expiration duration
func (s *StorageService) GetPresignedURLExpiry() time.Duration {
	return s.presignedURLExpiry
}

// GetLargeFileThreshold returns the configured large file threshold
func (s *StorageService) GetLargeFileThreshold() int64 {
	return s.largeFileThreshold
}

// IsLargeFile checks if a file size exceeds the large file threshold
// Requirements: 3.2 (Return pre-signed URL instead of streaming for large files)
func (s *StorageService) IsLargeFile(sizeBytes int64) bool {
	return sizeBytes >= s.largeFileThreshold
}
