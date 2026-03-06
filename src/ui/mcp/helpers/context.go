package helpers

import (
	"context"
	"fmt"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
)

// ContextWithDefaultDevice resolves the default device from the global DeviceManager
// and injects it into the provided context. This mirrors what the REST middleware does
// via Fiber locals, but for MCP handlers which lack HTTP middleware.
func ContextWithDefaultDevice(ctx context.Context) (context.Context, error) {
	mgr := whatsapp.GetDeviceManager()
	if mgr == nil {
		return ctx, fmt.Errorf("device manager not initialized")
	}

	inst, _, err := mgr.ResolveDevice("")
	if err != nil {
		return ctx, fmt.Errorf("device identification required: %w", err)
	}

	return whatsapp.ContextWithDevice(ctx, inst), nil
}
