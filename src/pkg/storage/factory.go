package storage

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

var globalStorage MediaStorage

// InitStorage initializes the global storage instance based on configuration
func InitStorage(storageType StorageType, localBasePath string, s3Config *S3Config) error {
	var storage MediaStorage
	var err error

	switch storageType {
	case StorageTypeLocal:
		storage, err = NewLocalStorage(localBasePath)
		if err != nil {
			return fmt.Errorf("failed to initialize local storage: %w", err)
		}
		logrus.Info("Initialized local file storage")

	case StorageTypeS3:
		if s3Config == nil {
			return fmt.Errorf("S3 configuration is required for S3 storage type")
		}

		// Use MinIO native SDK for better compatibility
		storage, err = NewMinIOStorage(*s3Config)
		if err != nil {
			return fmt.Errorf("failed to initialize MinIO storage: %w", err)
		}

		logrus.Infof("Initialized S3-compatible storage (endpoint: %s, bucket: %s)", s3Config.Endpoint, s3Config.Bucket)

	default:
		return fmt.Errorf("unsupported storage type: %s", storageType)
	}

	globalStorage = storage
	return nil
}

// GetStorage returns the global storage instance
func GetStorage() MediaStorage {
	if globalStorage == nil {
		logrus.Warn("Storage not initialized, using default local storage")
		// Fallback to local storage
		storage, err := NewLocalStorage("statics/media")
		if err != nil {
			logrus.Errorf("Failed to create fallback local storage: %v", err)
			return nil
		}
		globalStorage = storage
	}
	return globalStorage
}

// ParseStorageType converts a string to StorageType
func ParseStorageType(s string) (StorageType, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "local", "":
		return StorageTypeLocal, nil
	case "s3", "minio":
		return StorageTypeS3, nil
	default:
		return "", fmt.Errorf("invalid storage type: %s (valid options: local, s3)", s)
	}
}
