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

func (m *DeviceManager) getDeviceByJID(jid string) (*DeviceInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, inst := range m.devices {
		if inst != nil && inst.JID() == jid {
			return inst, true
		}
	}
	return nil, false
}

// IsHealthy returns true if the device manager is initialized and has a valid store connection.
// Note: This is a service initialization check, not a live connectivity check.
// Returning true indicates the internal store is ready, but does not guarantee
// that any WhatsApp device connections are currently active or authenticated.
func (m *DeviceManager) IsHealthy() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store != nil
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
		if inst, ok := m.getDeviceByJID(trimmedID); ok && inst != nil {
			return inst, inst.ID(), nil
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

// deleteStoreRowsForJID removes the whatsmeow device rows (primary + keys containers)
// whose JID matches jid. Matching uses the NonAD form to mirror LoadExistingDevices,
// where the devices table stores NonAD JIDs. It is idempotent — a row that is already
// gone is simply not found — and an empty jid is a no-op (a slot that was never paired
// has no store rows to delete).
func (m *DeviceManager) deleteStoreRowsForJID(ctx context.Context, jid string) error {
	if strings.TrimSpace(jid) == "" {
		return nil
	}

	var firstErr error
	deleteFrom := func(container *sqlstore.Container, label string) {
		if container == nil {
			return
		}
		devices, err := container.GetAllDevices(ctx)
		if err != nil {
			logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to enumerate %s devices for jid %s", label, jid)
			firstErr = errors.Join(firstErr, err)
			return
		}
		for _, dev := range devices {
			if dev == nil || dev.ID == nil {
				continue
			}
			if dev.ID.ToNonAD().String() != jid {
				continue
			}
			if err := container.DeleteDevice(ctx, dev); err != nil {
				logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete jid %s from %s store", jid, label)
				firstErr = errors.Join(firstErr, err)
			}
			break
		}
	}

	deleteFrom(m.store, "primary")
	if m.keys != nil && m.keys != m.store {
		deleteFrom(m.keys, "keys")
	}
	return firstErr
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

	// Resolve the device's WhatsApp JID before tearing anything down so we can delete
	// its whatsmeow store rows by JID even when no live client is attached.
	var jid string
	if inst, ok := m.GetDevice(deviceID); ok && inst != nil {
		jid = inst.JID()
		if cli := inst.GetClient(); cli != nil {
			// The WhatsApp unlink is best-effort: a dead/expired session may fail
			// here, but that must not block local cleanup or fail the purge.
			if err := cli.Logout(ctx); err != nil {
				logrus.WithError(err).Warnf("[DEVICE_MANAGER] remote unlink failed for device %s (best-effort)", deviceID)
			}
			cli.Disconnect()
		}
	}

	// Delete chatstorage data for this device (local cleanup — surfaced on failure).
	if m.storage != nil {
		if err := m.storage.DeleteDeviceData(deviceID); err != nil {
			logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete chatstorage for device %s", deviceID)
			recordErr(err)
		}
	}

	// Delete whatsmeow store/keys rows by JID (local cleanup — surfaced on failure).
	recordErr(m.deleteStoreRowsForJID(ctx, jid))

	// Remove from registry last
	m.RemoveDevice(deviceID)
	return firstErr
}

// LogoutDeviceKeepSlot logs the device out of WhatsApp (clearing its session/keys)
// but PRESERVES the device slot in the registry, so it keeps its id and display name
// and can be re-paired later under the same id. Unlike PurgeDevice, it does not remove
// the device record. Removing the slot entirely is the job of RemoveDevice (DELETE).
func (m *DeviceManager) LogoutDeviceKeepSlot(ctx context.Context, deviceID string) error {
	if deviceID == "" {
		return fmt.Errorf("device id is required")
	}

	inst, ok := m.GetDevice(deviceID)
	if !ok || inst == nil {
		return fmt.Errorf("device %s not found", deviceID)
	}

	if cli := inst.GetClient(); cli != nil {
		// Attempt the unlink whenever the client is paired (Store.ID set), not only when
		// IsLoggedIn: that is true only while connected, and skipping the attempt for a
		// momentarily-offline client would leave the phone showing the linked device
		// forever (the local session is deleted below, so it can never unlink later).
		// The WhatsApp unlink is best-effort: a dead/expired session may fail
		// here, but that must not block the local keep-slot cleanup below.
		if cli.Store != nil && cli.Store.ID != nil {
			if err := cli.Logout(ctx); err != nil {
				logrus.WithError(err).Warnf("[DEVICE_MANAGER] remote unlink failed for device %s (best-effort)", deviceID)
			}
		}
		cli.Disconnect()
	}

	return m.keepSlotLogout(ctx, deviceID)
}

// keepSlotLogout is the shared "logout but keep the slot" cleanup used by both explicit
// logout and the remote LoggedOut callbacks. It deletes the device's whatsmeow store
// rows (by JID, resolved before the reset clears it) and resets the in-memory client +
// persisted JID, keeping the slot (id + display name) for re-pairing. It does NOT call
// cli.Logout — explicit logout handles the unlink before delegating here, and a remote
// LoggedOut has already been unlinked on the phone.
func (m *DeviceManager) keepSlotLogout(ctx context.Context, deviceID string) error {
	inst, ok := m.GetDevice(deviceID)
	if !ok || inst == nil {
		// The remote-logout callback can hold a stale id: InitWaCLI keys its instance by
		// the AD JID string, but loadFromRegistry may replace it with a registry slot
		// keyed by uuid (instances store NonAD JIDs). Fall back to JID resolution so the
		// cleanup still lands on the surviving slot instead of leaving a stale JID and
		// an orphan keys-container row.
		if parsed, err := types.ParseJID(deviceID); err == nil && parsed.User != "" {
			inst, ok = m.getDeviceByJID(parsed.ToNonAD().String())
		}
		if !ok || inst == nil {
			return fmt.Errorf("device %s not found", deviceID)
		}
		deviceID = inst.ID()
	}

	// Resolve the JID before resetDeviceKeepSlot clears it, so we can delete the stored
	// whatsmeow rows even when no live client is attached (slot loaded from storage).
	// Always delete (don't rely on cli.Logout having done it): an orphan row would
	// otherwise get matched back on restart. Idempotent when the row is already gone.
	jid := inst.JID()

	var firstErr error
	firstErr = errors.Join(firstErr, m.deleteStoreRowsForJID(ctx, jid))
	firstErr = errors.Join(firstErr, m.resetDeviceKeepSlot(deviceID))
	return firstErr
}

// resetDeviceKeepSlot detaches the in-memory client and clears the persisted session
// identity (jid) while keeping the device slot (id + display name) in both the
// in-memory registry and the persisted device registry. EnsureClient rebuilds a
// fresh client on the next login, so the slot can be re-paired under the same id.
func (m *DeviceManager) resetDeviceKeepSlot(deviceID string) error {
	inst, ok := m.GetDevice(deviceID)
	if !ok || inst == nil {
		return fmt.Errorf("device %s not found", deviceID)
	}
	inst.ResetClient()

	if m.storage != nil && strings.TrimSpace(deviceID) != "" {
		if err := m.storage.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
			DeviceID:    deviceID,
			DisplayName: inst.DisplayName(),
			JID:         "",
			CreatedAt:   inst.CreatedAt(),
			UpdatedAt:   time.Now(),
		}); err != nil {
			return fmt.Errorf("persist logged-out device %s: %w", deviceID, err)
		}
	}
	return nil
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
		existingByID := m.devices[jid]
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
		if existingByID != nil {
			m.applyStoreJID(existingByID, jid)
			continue
		}
		if matchedDevice != nil {
			continue
		}

		// Match orphaned device with this JID
		if orphanDevice != nil {
			logrus.Infof("[DEVICE_MANAGER] matching orphaned device %s with JID %s", orphanDevice.ID(), jid)
			m.applyStoreJID(orphanDevice, jid)
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
		m.applyStoreJID(instance, jid)
		m.AddDevice(instance)
	}

	return nil
}

func (m *DeviceManager) applyStoreJID(instance *DeviceInstance, jid string) {
	if instance == nil || jid == "" {
		return
	}
	instance.mu.Lock()
	instance.jid = jid
	instance.mu.Unlock()
	instance.SetChatStorage(newDeviceChatStorage(jid, m.storage))
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
	if proxyURL := config.WhatsappProxy; proxyURL != "" {
		if err := client.SetProxyAddress(proxyURL); err != nil {
			baseLogger.Errorf("failed to apply WHATSAPP_PROXY=%q for device %s: %v", redactProxyURL(proxyURL), deviceID, err)
		} else {
			baseLogger.Infof("applied outbound proxy from WHATSAPP_PROXY for device %s", deviceID)
		}
	}
	client.EnableAutoReconnect = true
	client.AutoTrustIdentity = true

	repo := inst.GetChatStorage()
	if repo == nil {
		repo = newDeviceChatStorage(deviceID, m.storage)
		inst.SetChatStorage(repo)
	}

	client.AddEventHandler(func(rawEvt any) {
		handler(ctx, inst, rawEvt)
	})

	inst.SetOnLoggedOut(func(deviceID string) {
		// On remote logout (device unlinked from the phone) keep the slot so it can
		// be re-paired under the same id, matching the explicit logout semantics.
		// Use a fresh context: the original request ctx may already be cancelled.
		if err := m.keepSlotLogout(context.Background(), deviceID); err != nil {
			logrus.WithError(err).Warnf("[REMOTE_LOGOUT] keep-slot cleanup failed for %s", deviceID)
		}
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
			if dev, err := findStoreDeviceByJID(ctx, m.store, jid); err != nil {
				return nil, err
			} else if dev != nil {
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
				if dev, err := findStoreDeviceByJID(ctx, m.store, jid); err != nil {
					return nil, err
				} else if dev != nil {
					return dev, nil
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
	syncKeysDevice(ctx, m.store, m.keys, *device.ID)

	applyKeyCacheStore(device, innerStore)
	return nil
}

func findStoreDeviceByJID(ctx context.Context, container *sqlstore.Container, jid types.JID) (*store.Device, error) {
	if container == nil || jid.IsEmpty() {
		return nil, nil
	}

	if dev, err := container.GetDevice(ctx, jid); err != nil {
		return nil, err
	} else if dev != nil {
		return dev, nil
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return nil, err
	}

	targetJID := jid.ToNonAD().String()
	for _, dev := range devices {
		if dev != nil && dev.ID != nil && dev.ID.ToNonAD().String() == targetJID {
			return dev, nil
		}
	}
	return nil, nil
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

// GetStorage returns the chat storage repository.
func (m *DeviceManager) GetStorage() domainChatStorage.IChatStorageRepository {
	if m == nil {
		return nil
	}
	return m.storage
}
