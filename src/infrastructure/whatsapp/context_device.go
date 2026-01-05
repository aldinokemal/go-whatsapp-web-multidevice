package whatsapp

import (
	"context"

	"go.mau.fi/whatsmeow"
)

type deviceContextKey struct{}

// ContextWithDevice stores a device instance into the provided context for per-request scoping.
func ContextWithDevice(ctx context.Context, device *DeviceInstance) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithValue(ctx, deviceContextKey{}, device)
}

// DeviceFromContext retrieves a device instance from context if present.
func DeviceFromContext(ctx context.Context) (*DeviceInstance, bool) {
	if ctx == nil {
		return nil, false
	}
	if value := ctx.Value(deviceContextKey{}); value != nil {
		if inst, ok := value.(*DeviceInstance); ok {
			return inst, true
		}
	}
	return nil, false
}

// ClientFromContext returns the client stored in the device context.
// If a device was explicitly set in context but has no client (not logged in), returns nil.
// Only falls back to global client when no device was set in context (backward compatibility).
func ClientFromContext(ctx context.Context) *whatsmeow.Client {
	if inst, ok := DeviceFromContext(ctx); ok && inst != nil {
		// Device was explicitly set - return its client (may be nil if not logged in)
		return inst.GetClient()
	}
	// No device in context - fall back to global client for backward compatibility
	return GetClient()
}
