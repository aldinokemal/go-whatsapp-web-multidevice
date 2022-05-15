package utils

import (
	"os"
	"time"
)

func RemoveFile(delaySecond int, paths ...string) error {
	if delaySecond > 0 {
		time.Sleep(time.Duration(delaySecond) * time.Second)
	}

	for _, path := range paths {
		err := os.Remove(path)
		return err
	}
	return nil
}
