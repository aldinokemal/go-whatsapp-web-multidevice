package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

// CleanupDatabase removes the database file (SQLite) or deletes all devices (PostgreSQL) to prevent foreign key constraint issues
func CleanupDatabase() error {
	// Check if using PostgreSQL - for PostgreSQL we can use RLock since we're not closing connections
	if strings.HasPrefix(config.DBURI, "postgres:") {
		globalStateMu.RLock()
		currentDB := db
		currentKeysDB := keysDB
		globalStateMu.RUnlock()

		logrus.Info("[CLEANUP] PostgreSQL detected - deleting all devices from database")

		// Check if database is initialized
		if currentDB == nil {
			logrus.Warn("[CLEANUP] Database is nil, skipping device deletion")
			return nil
		}

		ctx := context.Background()

		// Get all devices
		devices, err := currentDB.GetAllDevices(ctx)
		if err != nil {
			logrus.Errorf("[CLEANUP] Error getting devices: %v", err)
			return fmt.Errorf("failed to get devices: %v", err)
		}

		logrus.Infof("[CLEANUP] Found %d devices to delete", len(devices))

		// Delete each device (this will cascade delete related records like identity keys, sessions, etc.)
		for _, device := range devices {
			logrus.Infof("[CLEANUP] Deleting device: %s", device.ID)
			if err := currentDB.DeleteDevice(ctx, device); err != nil {
				logrus.Errorf("[CLEANUP] Error deleting device %s: %v", device.ID, err)
				return fmt.Errorf("failed to delete device %s: %v", device.ID, err)
			}
		}

		// Also clean up keysDB if it exists and is separate
		if currentKeysDB != nil && currentKeysDB != currentDB {
			keysDevices, err := currentKeysDB.GetAllDevices(ctx)
			if err != nil {
				logrus.Errorf("[CLEANUP] Error getting devices from keysDB: %v", err)
				return fmt.Errorf("failed to get devices from keysDB: %v", err)
			}

			logrus.Infof("[CLEANUP] Found %d devices in keysDB to delete", len(keysDevices))

			for _, device := range keysDevices {
				logrus.Infof("[CLEANUP] Deleting device from keysDB: %s", device.ID)
				if err := currentKeysDB.DeleteDevice(ctx, device); err != nil {
					logrus.Errorf("[CLEANUP] Error deleting device %s from keysDB: %v", device.ID, err)
					return fmt.Errorf("failed to delete device %s from keysDB: %v", device.ID, err)
				}
			}
		}

		logrus.Info("[CLEANUP] All devices deleted successfully from PostgreSQL")
		return nil
	}

	// SQLite: Hold write lock for entire cleanup to prevent race conditions
	// Other goroutines must not access db/keysDB while we're closing and removing files
	globalStateMu.Lock()
	defer globalStateMu.Unlock()

	logrus.Info("[CLEANUP] SQLite detected - closing database connections before file removal")

	// Store references and clear globals immediately to prevent use-after-close
	currentDB := db
	currentKeysDB := keysDB
	db = nil
	keysDB = nil
	cli = nil // Also clear client since it depends on db

	// Close the main database connection
	if currentDB != nil {
		logrus.Info("[CLEANUP] Closing main database connection")
		if err := currentDB.Close(); err != nil {
			logrus.Errorf("[CLEANUP] Error closing main database: %v", err)
			return fmt.Errorf("failed to close main database: %v", err)
		}
		logrus.Info("[CLEANUP] Main database connection closed successfully")
	}

	// Close keysDB if it exists and is separate from main db
	if currentKeysDB != nil && currentKeysDB != currentDB {
		logrus.Info("[CLEANUP] Closing keysDB database connection")
		if err := currentKeysDB.Close(); err != nil {
			logrus.Errorf("[CLEANUP] Error closing keysDB: %v", err)
			return fmt.Errorf("failed to close keysDB: %v", err)
		}
		logrus.Info("[CLEANUP] KeysDB connection closed successfully")

		// Remove keysDB file if it's also SQLite
		if config.DBKeysURI != "" && strings.HasPrefix(config.DBKeysURI, "file:") {
			keysDBPath := strings.TrimPrefix(config.DBKeysURI, "file:")
			if strings.Contains(keysDBPath, "?") {
				keysDBPath = strings.Split(keysDBPath, "?")[0]
			}

			logrus.Infof("[CLEANUP] Removing keysDB file: %s", keysDBPath)
			if err := os.Remove(keysDBPath); err != nil {
				if !os.IsNotExist(err) {
					logrus.Errorf("[CLEANUP] Error removing keysDB file: %v", err)
					return fmt.Errorf("failed to remove keysDB file: %v", err)
				} else {
					logrus.Info("[CLEANUP] KeysDB file already removed")
				}
			} else {
				logrus.Info("[CLEANUP] KeysDB file removed successfully")
			}
		}
	}

	// Now remove the main database file
	dbPath := strings.TrimPrefix(config.DBURI, "file:")
	if strings.Contains(dbPath, "?") {
		dbPath = strings.Split(dbPath, "?")[0]
	}

	logrus.Infof("[CLEANUP] Removing main database file: %s", dbPath)
	if err := os.Remove(dbPath); err != nil {
		if !os.IsNotExist(err) {
			logrus.Errorf("[CLEANUP] Error removing database file: %v", err)
			return err
		} else {
			logrus.Info("[CLEANUP] Database file already removed")
		}
	} else {
		logrus.Info("[CLEANUP] Database file removed successfully")
	}
	return nil
}

// CleanupTemporaryFiles removes history files, QR images, and send items
func CleanupTemporaryFiles() error {
	// Clean up history files
	if files, err := filepath.Glob(fmt.Sprintf("./%s/history-*", config.PathStorages)); err == nil {
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				logrus.Errorf("[CLEANUP] Error removing history file %s: %v", f, err)
				return err
			}
		}
		logrus.Info("[CLEANUP] History files cleaned up")
	}

	// Clean up QR images
	if qrImages, err := filepath.Glob(fmt.Sprintf("./%s/scan-*", config.PathQrCode)); err == nil {
		for _, f := range qrImages {
			if err := os.Remove(f); err != nil {
				logrus.Errorf("[CLEANUP] Error removing QR image %s: %v", f, err)
				return err
			}
		}
		logrus.Info("[CLEANUP] QR images cleaned up")
	}

	// Clean up send items
	if qrItems, err := filepath.Glob(fmt.Sprintf("./%s/*", config.PathSendItems)); err == nil {
		for _, f := range qrItems {
			if !strings.Contains(f, ".gitignore") {
				if err := os.Remove(f); err != nil {
					logrus.Errorf("[CLEANUP] Error removing send item %s: %v", f, err)
					return err
				}
			}
		}
		logrus.Info("[CLEANUP] Send items cleaned up")
	}

	return nil
}

// ReinitializeWhatsAppComponents reinitializes database and client components
func ReinitializeWhatsAppComponents(ctx context.Context, chatStorageRepo domainChatStorage.IChatStorageRepository) (*sqlstore.Container, *whatsmeow.Client, error) {
	logrus.Info("[CLEANUP] Reinitializing database and client...")

	newDB := InitWaDB(ctx, config.DBURI)
	var newKeysDB *sqlstore.Container
	if config.DBKeysURI != "" {
		newKeysDB = InitWaDB(ctx, config.DBKeysURI)
	}
	newCli := InitWaCLI(ctx, newDB, newKeysDB, chatStorageRepo)

	logrus.Info("[CLEANUP] Database and client reinitialized successfully")

	return newDB, newCli, nil
}

// PerformCompleteCleanup performs all cleanup operations in the correct order
func PerformCompleteCleanup(ctx context.Context, logPrefix string, chatStorageRepo domainChatStorage.IChatStorageRepository) (*sqlstore.Container, *whatsmeow.Client, error) {
	logrus.Infof("[%s] Starting complete cleanup process...", logPrefix)

	// Disconnect current client if it exists
	if current := GetClient(); current != nil {
		current.Disconnect()
		logrus.Infof("[%s] Client disconnected", logPrefix)
	}

	// Truncate all chatstorage data before other cleanup
	if chatStorageRepo != nil {
		logrus.Infof("[%s] Truncating chatstorage data...", logPrefix)
		if err := chatStorageRepo.TruncateAllDataWithLogging(logPrefix); err != nil {
			logrus.Errorf("[%s] Failed to truncate chatstorage data: %v", logPrefix, err)
			// Continue with cleanup even if chatstorage truncation fails
		}
	}

	// Clean up database
	if err := CleanupDatabase(); err != nil {
		return nil, nil, fmt.Errorf("database cleanup failed: %v", err)
	}

	// Reinitialize components
	newDB, newCli, err := ReinitializeWhatsAppComponents(ctx, chatStorageRepo)
	if err != nil {
		return nil, nil, fmt.Errorf("reinitialization failed: %v", err)
	}

	// Clean up temporary files
	if err := CleanupTemporaryFiles(); err != nil {
		logrus.Errorf("[%s] Temporary file cleanup failed (non-critical): %v", logPrefix, err)
		// Don't return error for file cleanup as it's non-critical
	}

	logrus.Infof("[%s] Complete cleanup process finished successfully", logPrefix)
	logrus.Infof("[%s] Application is ready for next login without restart", logPrefix)

	return newDB, newCli, nil
}

// PerformCleanupAndUpdateGlobals is a convenience function that performs cleanup
// and ensures global client synchronization
func PerformCleanupAndUpdateGlobals(ctx context.Context, logPrefix string, chatStorageRepo domainChatStorage.IChatStorageRepository) (*sqlstore.Container, *whatsmeow.Client, error) {
	newDB, newCli, err := PerformCompleteCleanup(ctx, logPrefix, chatStorageRepo)
	if err != nil {
		return nil, nil, err
	}

	// Ensure global client is properly synchronized
	UpdateGlobalClient(newCli, newDB)

	return newDB, newCli, nil
}
