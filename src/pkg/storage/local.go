package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// LocalStorage implements MediaStorage interface for local file system storage
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new local storage instance
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	// Ensure the base path exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base path directory: %w", err)
	}

	return &LocalStorage{
		basePath: basePath,
	}, nil
}

// Save saves media data to local file system
func (s *LocalStorage) Save(ctx context.Context, data []byte, filename string) (string, error) {
	// Create full path
	fullPath := filepath.Join(s.basePath, filename)

	// Ensure the directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	logrus.Debugf("Saved media to local storage: %s", fullPath)
	return fullPath, nil
}

// SaveStream saves media from a reader stream to local file system
func (s *LocalStorage) SaveStream(ctx context.Context, reader io.Reader, filename string) (string, error) {
	// Create full path
	fullPath := filepath.Join(s.basePath, filename)

	// Ensure the directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Create file
	file, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy data from reader to file
	if _, err := io.Copy(file, reader); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	logrus.Debugf("Saved media stream to local storage: %s", fullPath)
	return fullPath, nil
}

// Get retrieves media data from local file system
func (s *LocalStorage) Get(ctx context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return data, nil
}

// Delete removes media from local file system
func (s *LocalStorage) Delete(ctx context.Context, path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	logrus.Debugf("Deleted media from local storage: %s", path)
	return nil
}

// GetURL returns the local file path
func (s *LocalStorage) GetURL(path string) string {
	return path
}
