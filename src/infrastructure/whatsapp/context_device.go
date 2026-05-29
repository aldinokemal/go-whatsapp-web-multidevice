package whatsapp

import (
	"context"

	"go.mau.fi/whatsmeow"
)

type deviceContextKey struct{}

type skipChatwootForwardKey struct{}

// ContextWithDevice stores a device instance into the provided context for per-request scoping.
func ContextWithDevice(ctx context.Context, device *DeviceInstance) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithValue(ctx, deviceContextKey{}, device)
}

// ContextWithSkipChatwootForward marks a context so messages sent under it are
// NOT forwarded back to Chatwoot. Set by the Chatwoot webhook handler on agent
// replies, which already originate from Chatwoot and would otherwise duplicate.
func ContextWithSkipChatwootForward(ctx context.Context) context.Context {
	if ctx == nil {
		return context.WithValue(context.Background(), skipChatwootForwardKey{}, true)
	}
	return context.WithValue(ctx, skipChatwootForwardKey{}, true)
}

// shouldSkipChatwootForward reports whether the context was flagged to skip the
// outgoing Chatwoot forward.
func shouldSkipChatwootForward(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	skip, _ := ctx.Value(skipChatwootForwardKey{}).(bool)
	return skip
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
