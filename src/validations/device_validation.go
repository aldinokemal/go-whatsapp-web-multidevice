package validations

import (
	"context"
	"strings"

	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

// ValidateDeviceID ensures a device id was provided before any logout/remove work.
// Device lifecycle routes (POST /devices/{device_id}/logout, DELETE /devices/{device_id})
// are addressed by path param and do not pass through the X-Device-Id header middleware,
// so the usecase validates the id here and operates on it via the device manager rather
// than resolving a WhatsApp client from context (which would fall back to the global
// client and break multi-device, and must work even when no live client is attached).
func ValidateDeviceID(_ context.Context, deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return pkgError.ValidationError("device_id is required")
	}
	return nil
}
