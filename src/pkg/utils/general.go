package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// PanicIfNeeded is panic if error is not nil
func PanicIfNeeded(err any, message ...string) {
	if err != nil {
		if fmt.Sprintf("%s", err) == "record not found" && len(message) > 0 {
			panic(message[0])
		} else {
			panic(err)
		}
	}
}

func StrToFloat64(text string) float64 {
	var result float64
	if text != "" {
		result, _ = strconv.ParseFloat(strings.TrimSpace(text), 64)
	}
	return result
}
