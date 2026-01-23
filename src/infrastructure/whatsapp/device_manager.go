package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// DeviceManager keeps a registry of active device instances.
type DeviceManager struct {
	mu       sync.RWMutex
	devices  map[string]*DeviceInstance
	store    *sqlstore.Container
	keys     *sqlstore.Container
	storage  domainChatStorage.IChatStorageRepository
	initted  bool
	initOnce sync.Once
}

func NewDeviceManager(store *sqlstore.Container, keys *sqlstore.Container, chatStorageRepo domainChatStorage.IChatStorageRepository) *DeviceManager {
	return &DeviceManager{
		devices: make(map[string]*DeviceInstance),
		store:   store,
		keys:    keys,
		storage: chatStorageRepo,
	}
}

func (m *DeviceManager) AddDevice(instance *DeviceInstance) {
	if instance == nil || instance.ID() == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[instance.ID()] = instance

	// Persist registry entry if available
	if m.storage != nil {
		_ = m.storage.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
			DeviceID:    instance.ID(),
			DisplayName: instance.DisplayName(),
			JID:         instance.JID(),
			CreatedAt:   instance.CreatedAt(),
			UpdatedAt:   time.Now(),
		})
	}
}

func (m *DeviceManager) GetDevice(id string) (*DeviceInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instance, ok := m.devices[id]
	return instance, ok
}

// DefaultDevice returns the only registered device when running in single-device mode.
func (m *DeviceManager) DefaultDevice() *DeviceInstance {
	if m == nil {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.devices) != 1 {
		return nil
	}

	for _, inst := range m.devices {
		return inst
	}

	return nil
}

// ResolveDevice attempts to locate a device by ID or falls back to the default/only device.
// It returns the resolved instance, the ID used, or an error when no suitable device is found.
func (m *DeviceManager) ResolveDevice(deviceID string) (*DeviceInstance, string, error) {
	if m == nil {
		return nil, "", fmt.Errorf("device manager not initialized")
	}

	trimmedID := strings.TrimSpace(deviceID)
	if trimmedID != "" {
		if inst, ok := m.GetDevice(trimmedID); ok && inst != nil {
			return inst, trimmedID, nil
		}
		return nil, trimmedID, fmt.Errorf("device %s not found", trimmedID)
	}

	if inst := m.DefaultDevice(); inst != nil {
		return inst, inst.ID(), nil
	}

	return nil, "", fmt.Errorf("device id is required")
}

func (m *DeviceManager) RemoveDevice(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.devices, id)

	if m.storage != nil && strings.TrimSpace(id) != "" {
		_ = m.storage.DeleteDeviceRecord(id)
	}
}

// PurgeDevice cleanly logs out a device, removes its persisted records (store/keys),
// deletes its chatstorage data, and removes it from the in-memory registry.
func (m *DeviceManager) PurgeDevice(ctx context.Context, deviceID string) error {
	if deviceID == "" {
		return fmt.Errorf("device id is required")
	}

	var firstErr error
	recordErr := func(err error) {
		if err != nil {
			firstErr = errors.Join(firstErr, err)
		}
	}

	// Attempt logout/disconnect if a client exists
	if inst, ok := m.GetDevice(deviceID); ok && inst != nil {
		if cli := inst.GetClient(); cli != nil {
			if err := cli.Logout(ctx); err != nil {
				logrus.WithError(err).Warnf("[DEVICE_MANAGER] logout failed for device %s", deviceID)
				recordErr(err)
			}
			cli.Disconnect()
		}
	}

	// Delete chatstorage data for this device
	if m.storage != nil {
		if err := m.storage.DeleteDeviceData(deviceID); err != nil {
			logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete chatstorage for device %s", deviceID)
			recordErr(err)
		}
	}

	// Remove device records from primary store
	if m.store != nil {
		if devices, err := m.store.GetAllDevices(ctx); err != nil {
			logrus.WithError(err).Warn("[DEVICE_MANAGER] failed to enumerate devices for purge")
			recordErr(err)
		} else {
			for _, dev := range devices {
				if dev != nil && dev.ID != nil && dev.ID.String() == deviceID {
					if err := m.store.DeleteDevice(ctx, dev); err != nil {
						logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete device %s from store", deviceID)
						recordErr(err)
					}
					break
				}
			}
		}
	}

	// Remove device records from keys store if separate
	if m.keys != nil && m.keys != m.store {
		if devices, err := m.keys.GetAllDevices(ctx); err != nil {
			logrus.WithError(err).Warn("[DEVICE_MANAGER] failed to enumerate keys devices for purge")
			recordErr(err)
		} else {
			for _, dev := range devices {
				if dev != nil && dev.ID != nil && dev.ID.String() == deviceID {
					if err := m.keys.DeleteDevice(ctx, dev); err != nil {
						logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete device %s from keys store", deviceID)
						recordErr(err)
					}
					break
				}
			}
		}
	}

	// Remove from registry last
	m.RemoveDevice(deviceID)
	return firstErr
}

// CreateDevice registers a new device placeholder so routes can be scoped strictly by device_id.
func (m *DeviceManager) CreateDevice(ctx context.Context, requestedID string) (*DeviceInstance, error) {
	if m == nil {
		return nil, fmt.Errorf("device manager not initialized")
	}

	id := requestedID
	if id == "" {
		id = fiberUtils.UUID()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.devices[id]; exists {
		return nil, fmt.Errorf("device %s already exists", id)
	}

	instance := NewDeviceInstance(id, nil, newDeviceChatStorage(id, m.storage))
	m.devices[id] = instance

	if m.storage != nil {
		if err := m.storage.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
			DeviceID:    id,
			DisplayName: instance.DisplayName(),
			JID:         instance.JID(),
			CreatedAt:   instance.CreatedAt(),
			UpdatedAt:   instance.CreatedAt(),
		}); err != nil {
			logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to persist device %s", id)
		}
	}

	logrus.WithContext(ctx).Infof("[DEVICE_MANAGER] created device placeholder %s", id)
	return instance, nil
}

func (m *DeviceManager) ListDevices() []*DeviceInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*DeviceInstance, 0, len(m.devices))
	for _, instance := range m.devices {
		result = append(result, instance)
	}

	// Sort by CreatedAt ascending (oldest first) for stable UI ordering.
	// Use ID as tie-breaker when CreatedAt is equal.
	slices.SortFunc(result, func(a, b *DeviceInstance) int {
		if cmp := a.CreatedAt().Compare(b.CreatedAt()); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.ID(), b.ID())
	})

	return result
}

// LoadExistingDevices registers existing device records in the store container without connecting them.
// This keeps the registry aware of all device IDs even before their clients are initialized.
func (m *DeviceManager) LoadExistingDevices(ctx context.Context) error {
	if m == nil || m.store == nil {
		return fmt.Errorf("device manager not initialized")
	}

	m.initOnce.Do(func() {
		m.initted = true
	})

	// Load from persisted registry
	if m.storage != nil {
		records, err := m.storage.ListDeviceRecords()
		if err != nil {
			logrus.WithError(err).Warn("[DEVICE_MANAGER] failed to load device registry")
		} else {
			logrus.Infof("[DEVICE_MANAGER] discovered %d device records in registry", len(records))
			m.loadFromRegistry(records)
		}
	}

	// Load from WhatsMeow store
	devices, err := m.store.GetAllDevices(ctx)
	if err != nil {
		return err
	}

	logrus.Infof("[DEVICE_MANAGER] discovered %d device records in store", len(devices))
	for _, dev := range devices {
		if dev == nil || dev.ID == nil {
			continue
		}
		// Use NonAD JID to match with devices table which stores NonAD format
		jid := dev.ID.ToNonAD().String()

		// Check if device already exists by ID or JID
		m.mu.RLock()
		_, existsByID := m.devices[jid]
		var matchedDevice *DeviceInstance
		var orphanDevice *DeviceInstance
		for _, inst := range m.devices {
			if inst.JID() == jid {
				matchedDevice = inst
				break
			}
			if inst.JID() == "" && orphanDevice == nil {
				orphanDevice = inst
			}
		}
		m.mu.RUnlock()

		// Skip if already matched
		if existsByID || matchedDevice != nil {
			continue
		}

		// Match orphaned device with this JID
		if orphanDevice != nil {
			logrus.Infof("[DEVICE_MANAGER] matching orphaned device %s with JID %s", orphanDevice.ID(), jid)
			orphanDevice.mu.Lock()
			orphanDevice.jid = jid
			orphanDevice.mu.Unlock()
			if m.storage != nil {
				_ = m.storage.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
					DeviceID: orphanDevice.ID(),
					JID:      jid,
				})
			}
			continue
		}

		// Create new device instance
		instance := NewDeviceInstance(jid, nil, newDeviceChatStorage(jid, m.storage))
		instance.SetState(domainDevice.DeviceStateDisconnected)
		m.AddDevice(instance)
	}

	return nil
}

// loadFromRegistry loads devices from the registry, handling deduplication.
func (m *DeviceManager) loadFromRegistry(records []*domainChatStorage.DeviceRecord) {
	// Collect JIDs from manual devices (device_id doesn't contain @)
	manualDeviceJIDs := make(map[string]bool)
	for _, rec := range records {
		if rec == nil || strings.TrimSpace(rec.DeviceID) == "" {
			continue
		}
		if !strings.Contains(rec.DeviceID, "@") && rec.JID != "" {
			manualDeviceJIDs[rec.JID] = true
		}
	}

	// Load devices, removing auto-created duplicates
	seenJIDs := make(map[string]bool)
	for _, rec := range records {
		if rec == nil || strings.TrimSpace(rec.DeviceID) == "" {
			continue
		}

		// Skip auto-created devices if manual device with same JID exists
		isAutoCreated := strings.Contains(rec.DeviceID, "@")
		if isAutoCreated && manualDeviceJIDs[rec.DeviceID] {
			logrus.Warnf("[DEVICE_MANAGER] removing auto-created device %s", rec.DeviceID)
			_ = m.storage.DeleteDeviceRecord(rec.DeviceID)
			continue
		}

		// Skip duplicate JIDs
		if rec.JID != "" {
			if seenJIDs[rec.JID] {
				logrus.Warnf("[DEVICE_MANAGER] removing duplicate JID device %s", rec.DeviceID)
				_ = m.storage.DeleteDeviceRecord(rec.DeviceID)
				continue
			}
			seenJIDs[rec.JID] = true
		}

		// Check if a device with this JID already exists in memory (from InitWaCLI)
		m.mu.RLock()
		var existingByJID *DeviceInstance
		for id, inst := range m.devices {
			if rec.JID != "" && inst.JID() == rec.JID && id != rec.DeviceID {
				existingByJID = inst
				break
			}
		}
		m.mu.RUnlock()

		// If device with matching JID exists, remove it and use the registry device
		if existingByJID != nil {
			m.mu.Lock()
			delete(m.devices, existingByJID.ID())
			m.mu.Unlock()
			logrus.Infof("[DEVICE_MANAGER] replacing in-memory device %s with registry device %s", existingByJID.ID(), rec.DeviceID)
		}

		// Create device instance
		storageDeviceID := rec.DeviceID
		if rec.JID != "" {
			storageDeviceID = rec.JID
		}
		instance := NewDeviceInstance(rec.DeviceID, nil, newDeviceChatStorage(storageDeviceID, m.storage))
		instance.SetState(domainDevice.DeviceStateDisconnected)
		instance.displayName = rec.DisplayName
		instance.jid = rec.JID

		// If we had an existing device with client, transfer the client
		if existingByJID != nil {
			if client := existingByJID.GetClient(); client != nil {
				instance.SetClient(client)
				instance.UpdateStateFromClient()
			}
		}

		m.AddDevice(instance)
	}
}

// EnsureDefault registers the current global client as the default device if present.
// It checks both by device ID and by JID to avoid creating duplicates.
func (m *DeviceManager) EnsureDefault(client *DeviceInstance) {
	if client == nil || client.ID() == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if device exists by ID
	if _, ok := m.devices[client.ID()]; ok {
		return
	}

	// Check if any existing device has matching JID
	clientJID := client.JID()
	if clientJID != "" {
		for _, inst := range m.devices {
			if inst.JID() == clientJID {
				// Update existing device with the new client
				inst.SetClient(client.GetClient())
				return
			}
		}
	}

	m.devices[client.ID()] = client
}

// EnsureClient returns a device instance with an initialized WhatsApp client.
// It lazily creates the underlying store device and registers event handlers.
func (m *DeviceManager) EnsureClient(ctx context.Context, deviceID string) (*DeviceInstance, error) {
	if m == nil {
		return nil, fmt.Errorf("device manager not initialized")
	}

	inst := m.ensureInstance(deviceID)
	if existing := inst.GetClient(); existing != nil {
		inst.UpdateStateFromClient()
		return inst, nil
	}

	storeDevice, err := m.getOrCreateStoreDevice(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	configureDeviceProps()

	if err := m.configureKeysStore(ctx, storeDevice); err != nil {
		return nil, fmt.Errorf("failed to configure keys store: %w", err)
	}

	baseLogger := waLog.Stdout(fmt.Sprintf("Client-%s", deviceID), config.WhatsappLogLevel, true)
	client := whatsmeow.NewClient(storeDevice, newFilteredLogger(baseLogger))
	client.EnableAutoReconnect = true
	client.AutoTrustIdentity = true

	repo := inst.GetChatStorage()
	if repo == nil {
		repo = newDeviceChatStorage(deviceID, m.storage)
		inst.SetChatStorage(repo)
	}

	client.AddEventHandler(func(rawEvt interface{}) {
		handler(ctx, inst, rawEvt)
	})

	inst.SetOnLoggedOut(func(deviceID string) {
		m.RemoveDevice(deviceID)
	})

	inst.SetClient(client)
	inst.UpdateStateFromClient()

	return inst, nil
}

func (m *DeviceManager) ensureInstance(deviceID string) *DeviceInstance {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check by device ID first
	if inst, ok := m.devices[deviceID]; ok {
		if inst.GetChatStorage() == nil {
			storageDeviceID := inst.JID()
			if storageDeviceID == "" {
				storageDeviceID = deviceID
			}
			inst.SetChatStorage(newDeviceChatStorage(storageDeviceID, m.storage))
		}
		return inst
	}

	// Check if any existing device has this as its JID (deviceID might be a JID)
	for _, inst := range m.devices {
		if inst.JID() == deviceID {
			if inst.GetChatStorage() == nil {
				storageDeviceID := inst.JID()
				if storageDeviceID == "" {
					storageDeviceID = inst.ID()
				}
				inst.SetChatStorage(newDeviceChatStorage(storageDeviceID, m.storage))
			}
			return inst
		}
	}

	inst := NewDeviceInstance(deviceID, nil, newDeviceChatStorage(deviceID, m.storage))
	m.devices[deviceID] = inst
	return inst
}

func (m *DeviceManager) getOrCreateStoreDevice(ctx context.Context, deviceID string) (*store.Device, error) {
	if m.store == nil {
		return nil, fmt.Errorf("store container is nil")
	}

	// Try to reuse an existing device record if the ID maps to a JID.
	if deviceID != "" {
		if jid, err := types.ParseJID(deviceID); err == nil {
			if dev, err := m.store.GetDevice(ctx, jid); err == nil && dev != nil {
				return dev, nil
			}
		}

		// If deviceID is not a valid JID, look up the device instance and use its JID
		m.mu.RLock()
		var instJID string
		if inst, ok := m.devices[deviceID]; ok && inst.JID() != "" {
			instJID = inst.JID()
		}
		m.mu.RUnlock()

		if instJID != "" {
			if jid, err := types.ParseJID(instJID); err == nil {
				if dev, err := m.store.GetDevice(ctx, jid); err == nil && dev != nil {
					return dev, nil
				}
				// Fallback: iterate all devices to find one with matching User (ignoring AD-ID)
				// This handles cases where registry has Non-AD JID but store has full JID
				if allDevices, err := m.store.GetAllDevices(ctx); err == nil {
					targetUser := jid.User
					for _, d := range allDevices {
						if d != nil && d.ID != nil && d.ID.User == targetUser {
							return d, nil
						}
					}
				}
			}
		}
	}

	return m.store.NewDevice(), nil
}

func (m *DeviceManager) configureKeysStore(ctx context.Context, device *store.Device) error {
	if m.keys == nil || device == nil || device.ID == nil {
		return nil
	}

	innerStore := sqlstore.NewSQLStore(m.keys, *device.ID)
	syncKeysDevice(ctx, m.store, m.keys)

	device.Identities = innerStore
	device.Sessions = innerStore
	device.PreKeys = innerStore
	device.SenderKeys = innerStore
	device.MsgSecrets = innerStore
	device.PrivacyTokens = innerStore
	return nil
}

func configureDeviceProps() {
	osName := fmt.Sprintf("%s %s", config.AppOs, config.AppVersion)
	store.DeviceProps.PlatformType = &config.AppPlatform
	store.DeviceProps.Os = &osName
}

// StoreInfo returns configured store URIs for observability.
func (m *DeviceManager) StoreInfo() (dbURI, keysURI string) {
	if m == nil {
		return "", ""
	}
	return config.DBURI, config.DBKeysURI
}
