package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Type definitions
type ExtractedMedia struct {
	MediaPath string `json:"media_path"`
	MimeType  string `json:"mime_type"`
	Caption   string `json:"caption"`
}

// Global variables
var (
	globalStateMu sync.RWMutex
	cli           *whatsmeow.Client
	db            *sqlstore.Container // Add global database reference for cleanup
	keysDB        *sqlstore.Container
	deviceManager *DeviceManager
	log           waLog.Logger
	startupTime   = time.Now().Unix()
)

func syncKeysDevice(ctx context.Context, db, keysDB *sqlstore.Container, jid types.JID) {
	if db == nil || keysDB == nil || jid.IsEmpty() {
		return
	}

	dev, err := findStoreDeviceByJID(ctx, db, jid)
	if err != nil {
		log.Errorf("Failed to find device for keys sync: %v", err)
		return
	}
	if dev == nil || dev.ID == nil {
		return
	}

	devices, err := keysDB.GetAllDevices(ctx)
	if err != nil {
		log.Errorf("Failed to get keys devices: %v", err)
		return
	}
	// Sibling companions of the same number are distinct sessions: a keys row for
	// companion :32 does not satisfy companion :28 (issue #760). Legacy bare-number
	// rows still match their AD counterpart.
	for _, existing := range devices {
		if existing != nil && existing.ID != nil && sameStoreIdentity(*existing.ID, *dev.ID) {
			return
		}
	}

	if err := keysDB.PutDevice(ctx, dev); err != nil {
		log.Errorf("Failed to sync keys device %s: %v", dev.ID.String(), err)
	}
}

// InitWaCLI initializes the WhatsApp client
func InitWaCLI(ctx context.Context, storeContainer, keysStoreContainer *sqlstore.Container, chatStorageRepo domainChatStorage.IChatStorageRepository) *whatsmeow.Client {
	device, err := storeContainer.GetFirstDevice(ctx)
	if err != nil {
		log.Errorf("Failed to get device: %v", err)
		panic(err)
	}

	if device == nil {
		log.Errorf("No device found")
		panic("No device found")
	}

	// Configure device properties
	osName := fmt.Sprintf("%s %s", config.AppOs, config.AppVersion)
	store.DeviceProps.PlatformType = &config.AppPlatform
	store.DeviceProps.Os = &osName

	// Keep references for global state update after client creation
	primaryDB := storeContainer
	keysContainer := keysStoreContainer

	// Configure a separated database for accelerating encryption caching
	if keysContainer != nil && device.ID != nil {
		innerStore := sqlstore.NewSQLStore(keysStoreContainer, *device.ID)

		syncKeysDevice(ctx, primaryDB, keysContainer, *device.ID)
		applyKeyCacheStore(device, innerStore)
	}

	instanceID := ""
	if device.ID != nil {
		instanceID = device.ID.String()
	}

	// Create and configure the client with filtered logging to avoid noisy reconnection EOF errors
	baseLogger := waLog.Stdout("Client", config.WhatsappLogLevel, true)
	client := whatsmeow.NewClient(device, newFilteredLogger(baseLogger))
	if proxyURL := config.WhatsappProxy; proxyURL != "" {
		if err := client.SetProxyAddress(proxyURL); err != nil {
			baseLogger.Errorf("failed to apply WHATSAPP_PROXY=%q: %v", redactProxyURL(proxyURL), err)
		} else {
			baseLogger.Infof("applied outbound proxy from WHATSAPP_PROXY")
		}
	}
	client.EnableAutoReconnect = true
	client.AutoTrustIdentity = true

	deviceRepo := newDeviceChatStorage(instanceID, chatStorageRepo)
	instance := NewDeviceInstance(instanceID, client, deviceRepo)

	client.AddEventHandler(func(rawEvt any) {
		handler(ctx, instance, rawEvt)
	})

	// Register device instance in the manager for multi-device awareness
	// Use EnsureDefault to avoid creating duplicates when a device with matching JID already exists
	if device.ID != nil {
		instanceID = device.ID.String()
	}
	dm := InitializeDeviceManager(storeContainer, keysStoreContainer, deviceRepo)
	if dm != nil && instanceID != "" {
		dm.EnsureDefault(instance)
		instance.SetOnLoggedOut(func(deviceID string) {
			// Route the startup path through the same keep-slot cleanup as the lazy
			// EnsureClient path, so a remote logout never deletes the device slot.
			if err := dm.keepSlotLogout(context.Background(), deviceID); err != nil {
				logrus.WithError(err).Warnf("[REMOTE_LOGOUT] keep-slot cleanup failed for %s", deviceID)
			}
		})
	}

	globalStateMu.Lock()
	cli = client
	db = primaryDB
	keysDB = keysContainer
	globalStateMu.Unlock()

	return client
}
