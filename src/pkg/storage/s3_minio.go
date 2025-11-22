package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/sirupsen/logrus"
)

// MinIOStorage implements MediaStorage interface using MinIO's native SDK
type MinIOStorage struct {
	client         *minio.Client
	bucket         string
	publicURL      string
	endpoint       string
	useServerProxy bool
	region         string
}

// NewMinIOStorage creates a new MinIO storage instance using native SDK
func NewMinIOStorage(cfg S3Config) (*MinIOStorage, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}

	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("S3 access key and secret key are required")
	}

	// Parse endpoint to remove protocol
	endpoint := cfg.Endpoint
	useSSL := true
	if strings.HasPrefix(endpoint, "https://") {
		endpoint = strings.TrimPrefix(endpoint, "https://")
		useSSL = true
	} else if strings.HasPrefix(endpoint, "http://") {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		useSSL = false
	}

	// Initialize MinIO client
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: useSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	logrus.Infof("Initialized MinIO storage with endpoint: %s, bucket: %s, region: %s, SSL: %v",
		endpoint, cfg.Bucket, cfg.Region, useSSL)

	// Log bucket access configuration
	if cfg.UseServerProxy {
		logrus.Infof("🔐 Using server proxy for private bucket access")
	} else {
		logrus.Infof("🌐 Using direct public bucket URLs")
	}

	return &MinIOStorage{
		client:         minioClient,
		bucket:         cfg.Bucket,
		publicURL:      cfg.PublicURL,
		endpoint:       cfg.Endpoint,
		useServerProxy: cfg.UseServerProxy,
		region:         cfg.Region,
	}, nil
}

// Save saves media data to MinIO
func (s *MinIOStorage) Save(ctx context.Context, data []byte, filename string) (string, error) {
	logrus.Debugf("📤 Attempting to save media to MinIO: filename=%s, size=%d bytes", filename, len(data))

	reader := bytes.NewReader(data)

	// Upload to MinIO
	uploadInfo, err := s.client.PutObject(ctx, s.bucket, filename, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		logrus.Errorf("❌ Failed to upload to MinIO: %v", err)
		return "", fmt.Errorf("failed to upload to MinIO: %w", err)
	}

	logrus.Debugf("✅ Successfully saved media to MinIO: bucket=%s, key=%s, etag=%s, size=%d",
		s.bucket, filename, uploadInfo.ETag, uploadInfo.Size)
	return filename, nil
}

// SaveStream saves media from a reader stream to MinIO
func (s *MinIOStorage) SaveStream(ctx context.Context, reader io.Reader, filename string) (string, error) {
	// Upload to MinIO with unknown size (-1)
	_, err := s.client.PutObject(ctx, s.bucket, filename, reader, -1, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload stream to MinIO: %w", err)
	}

	logrus.Debugf("Saved media stream to MinIO: bucket=%s, key=%s", s.bucket, filename)
	return filename, nil
}

// Get retrieves media data from MinIO
func (s *MinIOStorage) Get(ctx context.Context, path string) ([]byte, error) {
	// Download from MinIO
	object, err := s.client.GetObject(ctx, s.bucket, path, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object from MinIO: %w", err)
	}
	defer object.Close()

	// Read all data
	data, err := io.ReadAll(object)
	if err != nil {
		return nil, fmt.Errorf("failed to read MinIO object: %w", err)
	}

	return data, nil
}

// Delete removes media from MinIO
func (s *MinIOStorage) Delete(ctx context.Context, path string) error {
	err := s.client.RemoveObject(ctx, s.bucket, path, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object from MinIO: %w", err)
	}

	logrus.Debugf("Deleted media from MinIO: bucket=%s, key=%s", s.bucket, path)
	return nil
}

// GetURL returns a publicly accessible URL for the media
func (s *MinIOStorage) GetURL(path string) string {
	// If using server proxy for private bucket, return server download endpoint
	if s.useServerProxy {
		return fmt.Sprintf("/media/download/%s", path)
	}

	// If custom public URL is configured, use it for public bucket
	if s.publicURL != "" {
		base := strings.TrimRight(s.publicURL, "/")
		url := fmt.Sprintf("%s/%s/%s", base, s.bucket, path)
		logrus.Debugf("🔗 Public URL: %s", url)
		return url
	}

	// Otherwise, construct the direct S3/MinIO URL for public bucket
	base := strings.TrimRight(s.endpoint, "/")
	url := fmt.Sprintf("%s/%s/%s", base, s.bucket, path)
	logrus.Debugf("🔗 Public URL: %s", url)
	return url
}

// EnsureBucketExists checks if the bucket exists and creates it if it doesn't
func (s *MinIOStorage) EnsureBucketExists(ctx context.Context) error {
	// Check if bucket exists
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("failed to check if bucket exists: %w", err)
	}

	if exists {
		logrus.Infof("✓ MinIO bucket '%s' already exists", s.bucket)
		return nil
	}

	// Try to create bucket
	err = s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{
		Region: "us-east-1",
	})
	if err != nil {
		// Check if bucket was created by someone else in the meantime
		exists, existsErr := s.client.BucketExists(ctx, s.bucket)
		if existsErr == nil && exists {
			logrus.Infof("✓ MinIO bucket '%s' is accessible", s.bucket)
			return nil
		}
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	logrus.Infof("✓ Created MinIO bucket: %s", s.bucket)
	return nil
}

// TestConnection performs a comprehensive test of MinIO connectivity
func (s *MinIOStorage) TestConnection(ctx context.Context) error {
	logrus.Info("Testing MinIO connection...")

	// Test 1: List buckets
	logrus.Debug("Test 1: Listing buckets...")
	buckets, err := s.client.ListBuckets(ctx)
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}
	logrus.Infof("✓ Successfully listed %d buckets", len(buckets))

	// Test 2: Check if our bucket exists
	logrus.Debugf("Test 2: Checking if bucket '%s' exists...", s.bucket)
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("bucket '%s' does not exist", s.bucket)
	}
	logrus.Infof("✓ Bucket '%s' exists", s.bucket)

	// Test 3: Upload test object
	logrus.Debug("Test 3: Uploading test object...")
	testKey := fmt.Sprintf("test/connection-test-%d.txt", ctx.Value("timestamp"))
	testData := []byte("Connection test from WhatsApp Web API")

	_, err = s.client.PutObject(ctx, s.bucket, testKey, bytes.NewReader(testData),
		int64(len(testData)), minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to upload test object: %w", err)
	}
	logrus.Infof("✓ Successfully uploaded test object: %s", testKey)

	// Test 4: Download test object
	logrus.Debug("Test 4: Downloading test object...")
	object, err := s.client.GetObject(ctx, s.bucket, testKey, minio.GetObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to get test object: %w", err)
	}
	object.Close()
	logrus.Info("✓ Successfully downloaded test object")

	// Test 5: Delete test object
	logrus.Debug("Test 5: Deleting test object...")
	err = s.client.RemoveObject(ctx, s.bucket, testKey, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete test object: %w", err)
	}
	logrus.Info("✓ Successfully deleted test object")

	logrus.Info("✅ All MinIO connection tests passed!")
	return nil
}
