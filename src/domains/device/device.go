package device

import "time"

// DeviceState represents the connection lifecycle of a WhatsApp device.
// States are intentionally string-based for easy logging and JSON responses.
type DeviceState string

const (
	DeviceStateDisconnected DeviceState = "disconnected"
	DeviceStateConnecting   DeviceState = "connecting"
	DeviceStateConnected    DeviceState = "connected"
	DeviceStateLoggedIn     DeviceState = "logged_in"
)

// Device describes a WhatsApp account/device tracked by the system.
type Device struct {
	ID          string      `json:"id"`
	PhoneNumber string      `json:"phone_number,omitempty"`
	DisplayName string      `json:"display_name,omitempty"`
	State       DeviceState `json:"state"`
	JID         string      `json:"jid,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}
