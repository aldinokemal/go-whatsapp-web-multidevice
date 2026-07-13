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
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	fiberUtils "github.com/gofiber/utils/v2"
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
			ADJID:       instance.ADJID(),
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
	// Prefer the exact companion identity so two slots on the same number stay distinct.
	for _, inst := range m.devices {
		if inst != nil && inst.ADJID() != "" && inst.ADJID() == jid {
			return inst, true
		}
	}
	for _, inst := range m.devices {
		if inst != nil && inst.JID() == jid {
			return inst, true
		}
	}
	return nil, false
}

// storeIdentity returns the most precise store identity a slot knows: the full AD JID
// once the companion suffix is known, otherwise the bare-number JID.
func storeIdentity(inst *DeviceInstance) string {
	if inst == nil {
		return ""
	}
	if ad := inst.ADJID(); ad != "" {
		return ad
	}
	return inst.JID()
}

// sameStoreIdentity reports whether two store JIDs refer to the same companion
// session. JIDs with explicit device suffixes must match exactly; a bare-number JID
// (legacy rows/lookups that never carried the :NN suffix) matches any companion of
// that number.
func sameStoreIdentity(a, b types.JID) bool {
	if a.ToNonAD() != b.ToNonAD() {
		return false
	}
	if a.Device != 0 && b.Device != 0 {
		return a.Device == b.Device
	}
	return true
}

// slotIdentityMatches reports whether an existing slot refers to the same companion
// session as the given identity. When both sides know their AD JID it must match
// exactly (two slots may share a number); otherwise the bare number decides (records
// predating the ad_jid column).
func slotIdentityMatches(inst *DeviceInstance, nonAD, adJID string) bool {
	if inst == nil {
		return false
	}
	instAD := inst.ADJID()
	if adJID != "" && instAD != "" {
		return instAD == adJID
	}
	return nonAD != "" && inst.JID() == nonAD
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
// matching the given identity. A full AD JID (number:NN@s.whatsapp.net) deletes exactly
// that companion session; a bare-number JID (legacy slots that never stored the AD
// form) deletes only when it matches a single row — several sibling companion rows for
// one number are ambiguous, and deleting here could kill another slot's live session
// (issue #760). It is idempotent — a row that is already gone is simply not found —
// and an empty jid is a no-op (a slot that was never paired has no store rows).
func (m *DeviceManager) deleteStoreRowsForJID(ctx context.Context, jid string) error {
	if strings.TrimSpace(jid) == "" {
		return nil
	}
	target, err := types.ParseJID(jid)
	if err != nil || target.User == "" {
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
		var matches []*store.Device
		for _, dev := range devices {
			if dev == nil || dev.ID == nil {
				continue
			}
			if dev.ID.ToNonAD() != target.ToNonAD() {
				continue
			}
			// Deletion is stricter than sync matching: an AD target deletes only its
			// exact companion row. A bare-number (Device-0) row of the same number may
			// be a legacy slot's usable session and must never be taken along.
			if target.Device != 0 && dev.ID.Device != target.Device {
				continue
			}
			matches = append(matches, dev)
		}
		if target.Device == 0 && len(matches) > 1 {
			logrus.Warnf("[DEVICE_MANAGER] %d companion sessions in %s store match %s; skipping delete to avoid removing a sibling slot's session", len(matches), label, jid)
			return
		}
		for _, dev := range matches {
			if err := container.DeleteDevice(ctx, dev); err != nil {
				logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete jid %s from %s store", jid, label)
				firstErr = errors.Join(firstErr, err)
			}
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

	// Resolve the device's WhatsApp identity before tearing anything down so we can
	// delete its whatsmeow store rows even when no live client is attached.
	var jid string
	if inst, ok := m.GetDevice(deviceID); ok && inst != nil {
		jid = storeIdentity(inst)
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

		// Drop the device's Chatwoot config (and its message links) with it. An
		// orphaned row would keep claiming the device's JID under the unique
		// device_jid index, blocking a re-created device for the same number from
		// being configured.
		if cfg, err := m.storage.GetChatwootDeviceConfig(deviceID); err != nil {
			logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to load chatwoot config for device %s", deviceID)
			recordErr(err)
		} else if cfg != nil {
			if err := m.storage.DeleteChatwootDeviceConfig(deviceID); err != nil {
				logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete chatwoot config for device %s", deviceID)
				recordErr(err)
			} else {
				if cfg.ID != 0 {
					if err := m.storage.DeleteChatwootMessageLinksByConfig(cfg.ID); err != nil {
						logrus.WithError(err).Warnf("[DEVICE_MANAGER] failed to delete chatwoot links for device %s", deviceID)
						recordErr(err)
					}
				}
				if reg := chatwoot.GetClientRegistry(); reg != nil {
					reg.Invalidate(deviceID)
				}
			}
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
		// keyed by uuid. Fall back to JID resolution — exact AD JID first so siblings on
		// the same number stay distinct, then the bare number for legacy slots — so the
		// cleanup still lands on the surviving slot instead of leaving a stale JID and
		// an orphan keys-container row.
		if parsed, err := types.ParseJID(deviceID); err == nil && parsed.User != "" {
			inst, ok = m.getDeviceByJID(parsed.String())
			if !ok {
				inst, ok = m.getDeviceByJID(parsed.ToNonAD().String())
			}
		}
		if !ok || inst == nil {
			return fmt.Errorf("device %s not found", deviceID)
		}
		deviceID = inst.ID()
	}

	// Resolve the identity before resetDeviceKeepSlot clears it, so we can delete the
	// stored whatsmeow rows even when no live client is attached (slot loaded from
	// storage). Always delete (don't rely on cli.Logout having done it): an orphan row
	// would otherwise get matched back on restart. Idempotent when the row is already gone.
	jid := storeIdentity(inst)

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
			ADJID:       "",
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
		adJID := dev.ID.String()
		nonAD := dev.ID.ToNonAD().String()

		m.mu.RLock()
		var exactMatch, legacyMatch, orphanSlot *DeviceInstance
		numberClaimed := false
		for _, inst := range m.devices {
			switch {
			case inst.ADJID() == adJID:
				exactMatch = inst
			case inst.JID() == nonAD:
				numberClaimed = true
				if inst.ADJID() == "" && legacyMatch == nil {
					legacyMatch = inst
				}
			case inst.JID() == "" && inst.ADJID() == "" && orphanSlot == nil:
				orphanSlot = inst
			}
		}
		m.mu.RUnlock()

		// Slot already mapped to this exact companion session.
		if exactMatch != nil {
			m.applyStoreJID(exactMatch, *dev.ID)
			continue
		}

		// Legacy slot that only stored the bare number: backfill the companion identity
		// from the store row. When a number has several rows and no recorded AD JIDs the
		// first row wins — that ambiguity predates the ad_jid column.
		if legacyMatch != nil {
			logrus.Infof("[DEVICE_MANAGER] backfilling companion %s for device %s", adJID, legacyMatch.ID())
			m.applyStoreJID(legacyMatch, *dev.ID)
			m.persistInstanceRecord(legacyMatch)
			continue
		}

		// Another slot already claims this number with a different companion: leave the
		// row alone instead of adopting it — dialing it would churn against a dead or
		// sibling session (issue #760).
		if numberClaimed {
			logrus.Warnf("[DEVICE_MANAGER] leaving unreferenced companion session %s alone: number already claimed by another slot", adJID)
			continue
		}

		// Match orphaned device (registered slot without a session) with this row.
		if orphanSlot != nil {
			logrus.Infof("[DEVICE_MANAGER] matching orphaned device %s with JID %s", orphanSlot.ID(), nonAD)
			m.applyStoreJID(orphanSlot, *dev.ID)
			m.persistInstanceRecord(orphanSlot)
			continue
		}

		// Create new device instance
		instance := NewDeviceInstance(nonAD, nil, newDeviceChatStorage(nonAD, m.storage))
		instance.SetState(domainDevice.DeviceStateDisconnected)
		m.applyStoreJID(instance, *dev.ID)
		m.AddDevice(instance)
	}

	return nil
}

// applyStoreJID stamps a store row's identity onto a slot: the bare number as the
// chat-storage partition key and the full AD JID as the companion identity.
func (m *DeviceManager) applyStoreJID(instance *DeviceInstance, storeJID types.JID) {
	if instance == nil || storeJID.IsEmpty() {
		return
	}
	nonAD := storeJID.ToNonAD().String()
	instance.mu.Lock()
	instance.jid = nonAD
	instance.adJID = storeJID.String()
	instance.mu.Unlock()
	instance.SetChatStorage(newDeviceChatStorage(nonAD, m.storage))
}

// persistInstanceRecord upserts the slot's registry record from its in-memory state.
func (m *DeviceManager) persistInstanceRecord(inst *DeviceInstance) {
	if m.storage == nil || inst == nil || strings.TrimSpace(inst.ID()) == "" {
		return
	}
	_ = m.storage.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
		DeviceID:    inst.ID(),
		DisplayName: inst.DisplayName(),
		JID:         inst.JID(),
		ADJID:       inst.ADJID(),
		CreatedAt:   inst.CreatedAt(),
		UpdatedAt:   time.Now(),
	})
}

// loadFromRegistry loads devices from the registry. Boot reconciliation never deletes
// registry records: two slots may legitimately share a phone number as distinct
// companion sessions (issue #760), and even a genuinely stale record is only skipped
// here — deletion belongs to explicit remove/purge.
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

	// The AD JID identifies the exact companion, so two slots on the same number are
	// distinct identities. Only records predating the ad_jid column fall back to the
	// bare number, where a duplicate is ambiguous and skipped (never deleted).
	seenIdentities := make(map[string]bool)
	for _, rec := range records {
		if rec == nil || strings.TrimSpace(rec.DeviceID) == "" {
			continue
		}

		// Skip auto-created devices if manual device with same JID exists
		isAutoCreated := strings.Contains(rec.DeviceID, "@")
		if isAutoCreated && manualDeviceJIDs[rec.DeviceID] {
			logrus.Warnf("[DEVICE_MANAGER] skipping auto-created device %s: a named slot claims this number", rec.DeviceID)
			continue
		}

		identity := rec.ADJID
		if identity == "" {
			identity = rec.JID
		}
		if identity != "" {
			if seenIdentities[identity] {
				logrus.Warnf("[DEVICE_MANAGER] skipping device %s: identity %s already loaded (record kept; remove the slot explicitly if stale)", rec.DeviceID, identity)
				continue
			}
			seenIdentities[identity] = true
		}

		// Check if a device with this identity already exists in memory (from InitWaCLI)
		m.mu.RLock()
		var existingByJID *DeviceInstance
		for id, inst := range m.devices {
			if id != rec.DeviceID && slotIdentityMatches(inst, rec.JID, rec.ADJID) {
				existingByJID = inst
				break
			}
		}
		m.mu.RUnlock()

		// If device with matching identity exists, remove it and use the registry device
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
		instance.adJID = rec.ADJID

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

	// Check if any existing device refers to the same companion session
	clientJID := client.JID()
	if clientJID != "" {
		for _, inst := range m.devices {
			if slotIdentityMatches(inst, clientJID, client.ADJID()) {
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
		if inst.JID() == deviceID || (inst.ADJID() != "" && inst.ADJID() == deviceID) {
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

	// Try to reuse an existing device record. The slot's own identity comes first: its
	// AD JID pins the exact companion session, while deviceID may itself be a JID.
	if deviceID != "" {
		m.mu.RLock()
		var instJID string
		if inst, ok := m.devices[deviceID]; ok {
			instJID = storeIdentity(inst)
		}
		m.mu.RUnlock()

		candidates := make([]string, 0, 2)
		if instJID != "" {
			candidates = append(candidates, instJID)
		}
		if deviceID != instJID {
			candidates = append(candidates, deviceID)
		}

		for _, candidate := range candidates {
			jid, err := types.ParseJID(candidate)
			if err != nil || jid.User == "" {
				continue
			}
			dev, err := findStoreDeviceByJID(ctx, m.store, jid)
			if err != nil {
				return nil, err
			}
			if dev == nil || dev.ID == nil {
				continue
			}
			// Never hand a slot a companion session that a sibling slot already claims —
			// that is a session hijack (issue #760). Fall through to a fresh device so
			// the slot can be re-paired instead.
			if m.companionClaimedByOther(deviceID, dev.ID.String()) {
				logrus.Warnf("[DEVICE_MANAGER] companion session %s is claimed by another slot; giving %s a fresh session", dev.ID.String(), deviceID)
				continue
			}
			return dev, nil
		}
	}

	return m.store.NewDevice(), nil
}

// companionClaimedByOther reports whether a slot other than deviceID has recorded the
// given companion identity as its own.
func (m *DeviceManager) companionClaimedByOther(deviceID, adJID string) bool {
	if adJID == "" {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, inst := range m.devices {
		if id != deviceID && inst != nil && inst.ADJID() == adJID {
			return true
		}
	}
	return false
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

	// A JID with an explicit device suffix identifies one exact companion session; a
	// miss means that session is gone. Falling back to bare-number matching here could
	// silently return a sibling companion of the same number (issue #760).
	if jid.Device != 0 {
		return nil, nil
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		return nil, err
	}

	// Bare-number lookup (legacy slots that never stored the AD JID): resolve only
	// when it is unambiguous. Several companion rows for one number → never guess.
	var matches []*store.Device
	targetJID := jid.ToNonAD().String()
	for _, dev := range devices {
		if dev != nil && dev.ID != nil && dev.ID.ToNonAD().String() == targetJID {
			matches = append(matches, dev)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		logrus.Warnf("[DEVICE_MANAGER] %d companion sessions match %s; refusing to guess (re-pair the slot or purge the orphans)", len(matches), targetJID)
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
