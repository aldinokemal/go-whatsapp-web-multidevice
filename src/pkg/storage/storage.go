package storage

import (
	"context"
	"io"
)

// MediaStorage defines the interface for media storage operations
type MediaStorage interface {
	// Save saves media data to storage and returns the storage path/URL
	Save(ctx context.Context, data []byte, filename string) (path string, err error)

	// Get retrieves media data from storage
	Get(ctx context.Context, path string) (data []byte, err error)

	// Delete removes media from storage
	Delete(ctx context.Context, path string) error

	// GetURL returns a publicly accessible URL for the media
	// For local storage, this returns the local file path
	// For S3 storage, this returns the S3 URL or public URL if configured
	GetURL(path string) string

	// SaveStream saves media from a reader stream to storage
	SaveStream(ctx context.Context, reader io.Reader, filename string) (path string, err error)
}

// StorageType represents the type of storage backend
type StorageType string

const (
	// StorageTypeLocal represents local file system storage
	StorageTypeLocal StorageType = "local"

	// StorageTypeS3 represents S3/MinIO storage
	StorageTypeS3 StorageType = "s3"
)

// S3Config holds configuration for S3/MinIO storage
type S3Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	ForcePathStyle  bool
	PublicURL       string // Optional: custom public URL for public bucket access
	UseServerProxy  bool   // If true, use server download endpoint for private bucket access
}
