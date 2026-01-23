package whatsapp

import (
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
)

// UpdateGlobalClient updates the global cli variable with a new client instance
// This is needed when reinitializing the client after logout to ensure all
// infrastructure code uses the new client instance
func UpdateGlobalClient(newCli *whatsmeow.Client, newDB *sqlstore.Container) {
	globalStateMu.Lock()
	cli = newCli
	db = newDB
	globalStateMu.Unlock()
	log.Infof("Global WhatsApp client updated successfully")
}

// GetClient returns the current global client instance (alias for GetGlobalClient)
func GetClient() *whatsmeow.Client {
	globalStateMu.RLock()
	defer globalStateMu.RUnlock()
	return cli
}

// Get DB instance
func GetDB() *sqlstore.Container {
	globalStateMu.RLock()
	defer globalStateMu.RUnlock()
	return db
}

func getStoreContainers() (*sqlstore.Container, *sqlstore.Container) {
	globalStateMu.RLock()
	defer globalStateMu.RUnlock()
	return db, keysDB
}

// InitializeDeviceManager creates the global DeviceManager if it doesn't exist.
func InitializeDeviceManager(storeContainer, keysStoreContainer *sqlstore.Container, chatStorageRepo domainChatStorage.IChatStorageRepository) *DeviceManager {
	globalStateMu.Lock()
	defer globalStateMu.Unlock()
	if deviceManager == nil {
		deviceManager = NewDeviceManager(storeContainer, keysStoreContainer, chatStorageRepo)
	}
	return deviceManager
}

// GetDeviceManager returns the global DeviceManager.
func GetDeviceManager() *DeviceManager {
	globalStateMu.RLock()
	defer globalStateMu.RUnlock()
	return deviceManager
}
