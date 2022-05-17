package utils

import (
	"os"
	"path/filepath"
	"time"
)

// RemoveFile is removing file with delay
func RemoveFile(delaySecond int, paths ...string) error {
	if delaySecond > 0 {
		time.Sleep(time.Duration(delaySecond) * time.Second)
	}

	for _, path := range paths {
		if path != "" {
			err := os.Remove(path)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateFolder create new folder and sub folder if not exist
func CreateFolder(folderPath ...string) error {
	for _, folder := range folderPath {
		newFolder := filepath.Join(".", folder)
		err := os.MkdirAll(newFolder, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}
