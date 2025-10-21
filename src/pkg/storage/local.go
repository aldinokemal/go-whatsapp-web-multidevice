package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	// Normalize key and validate
	key := filepath.Clean(filename)
	if key == "." || key == "" || filepath.IsAbs(key) || strings.Contains(key, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid filename")
	}
	fullPath := filepath.Join(s.basePath, key)
	absBase, _ := filepath.Abs(s.basePath)
	absFull, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absFull, absBase+string(os.PathSeparator)) && absFull != absBase {
		return "", fmt.Errorf("path escapes base directory")
	}

	// Ensure the directory exists
	dir := filepath.Dir(absFull)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(absFull, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	logrus.WithField("key", key).Debug("Saved media to local storage")
	return key, nil
}

// SaveStream saves media from a reader stream to local file system
func (s *LocalStorage) SaveStream(ctx context.Context, reader io.Reader, filename string) (string, error) {
	key := filepath.Clean(filename)
	if key == "." || key == "" || filepath.IsAbs(key) || strings.Contains(key, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid filename")
	}
	fullPath := filepath.Join(s.basePath, key)
	absBase, _ := filepath.Abs(s.basePath)
	absFull, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absFull, absBase+string(os.PathSeparator)) && absFull != absBase {
		return "", fmt.Errorf("path escapes base directory")
	}

	// Ensure the directory exists
	dir := filepath.Dir(absFull)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Create file
	file, err := os.Create(absFull)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy data from reader to file
	if _, err := io.Copy(file, reader); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	logrus.WithField("key", key).Debug("Saved media stream to local storage")
	return key, nil
}

// Get retrieves media data from local file system
func (s *LocalStorage) Get(ctx context.Context, path string) ([]byte, error) {
	key := filepath.Clean(path)
	if key == "." || key == "" || filepath.IsAbs(key) || strings.Contains(key, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("invalid path")
	}
	fullPath := filepath.Join(s.basePath, key)
	absBase, _ := filepath.Abs(s.basePath)
	absFull, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absFull, absBase+string(os.PathSeparator)) && absFull != absBase {
		return nil, fmt.Errorf("path escapes base directory")
	}
	data, err := os.ReadFile(absFull)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return data, nil
}

// Delete removes media from local file system
func (s *LocalStorage) Delete(ctx context.Context, path string) error {
	key := filepath.Clean(path)
	if key == "." || key == "" || filepath.IsAbs(key) || strings.Contains(key, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("invalid path")
	}
	fullPath := filepath.Join(s.basePath, key)
	absBase, _ := filepath.Abs(s.basePath)
	absFull, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absFull, absBase+string(os.PathSeparator)) && absFull != absBase {
		return fmt.Errorf("path escapes base directory")
	}
	if err := os.Remove(absFull); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	logrus.WithField("key", key).Debug("Deleted media from local storage")
	return nil
}

// GetURL returns a web-friendly URL for the local file
func (s *LocalStorage) GetURL(path string) string {
	// Return relative URL for static file serving
	// Clean path and convert to forward slashes for web compatibility
	key := filepath.ToSlash(filepath.Clean(path))
	// Prefix with '/' so clients can fetch it via the static file server
	return "/" + filepath.ToSlash(filepath.Join(s.basePath, key))
}
