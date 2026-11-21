package whatsapp

import (
	"context"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

// Resolve ownership at admission: a startup client can move into a persisted slot.
// No registry lock is held while handlers perform network, storage or websocket work.
func (m *DeviceManager) eventHandler(client *whatsmeow.Client) func(any) {
	return func(evt any) {
		if owner := m.eventOwner(client); owner != nil {
			handler(context.Background(), owner, evt)
		}
	}
}

func (m *DeviceManager) eventOwner(client *whatsmeow.Client) *DeviceInstance {
	if client == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var owner *DeviceInstance
	for _, instance := range m.devices {
		if instance != nil && instance.GetClient() == client {
			if owner != nil {
				return nil
			}
			owner = instance
		}
	}
	if m.transient != nil && m.transient.GetClient() == client {
		if owner != nil {
			return nil
		}
		owner = m.transient
	}
	return owner
}

// Caller holds m.mu before taking any instance lock.
func (m *DeviceManager) attachClientLocked(instance *DeviceInstance, client *whatsmeow.Client) {
	if m.transient != nil && m.transient != instance && client != nil && m.transient.GetClient() == client {
		m.transient.ResetClient()
		m.transient = nil
	}
	if instance.GetClient() != client {
		instance.SetClient(client)
		instance.UpdateStateFromClient()
	}
	instance.SetOnLoggedOut(func(deviceID string) {
		if deviceID == "" {
			m.mu.Lock()
			if m.transient != instance {
				m.mu.Unlock()
				return
			}
			jid := storeIdentity(instance)
			instance.ResetClient()
			m.transient = nil
			m.mu.Unlock()
			if err := m.deleteStoreRowsForJID(context.Background(), jid); err != nil {
				logrus.WithError(err).Warn("[REMOTE_LOGOUT] transient cleanup failed")
			}
			return
		}
		if err := m.keepSlotLogout(context.Background(), deviceID); err != nil {
			logrus.WithError(err).Warnf("[REMOTE_LOGOUT] keep-slot cleanup failed for %s", deviceID)
		}
	})
}
