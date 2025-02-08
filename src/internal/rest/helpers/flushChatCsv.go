package helpers

import (
	"os"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/sirupsen/logrus"
)

var flushMutex sync.Mutex

func FlushChatCsv() error {
	flushMutex.Lock()
	defer flushMutex.Unlock()

	// Create an empty file (truncating any existing content)
	file, err := os.OpenFile(config.PathChatStorage, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	return nil
}

// StartAutoFlushChatStorage starts a goroutine that periodically flushes the chat storage
func StartAutoFlushChatStorage() {
	interval := time.Duration(config.AppChatFlushIntervalDays) * 24 * time.Hour

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if err := FlushChatCsv(); err != nil {
				logrus.Errorf("Error flushing chat storage: %v", err)
			} else {
				logrus.Info("Successfully flushed chat storage")
			}
		}
	}()

	logrus.Infof("Auto flush for chat storage started (your account chat still safe). Will flush every %d days", config.AppChatFlushIntervalDays)
}
