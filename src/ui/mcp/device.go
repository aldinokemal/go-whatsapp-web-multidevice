package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	mcpg "github.com/mark3labs/mcp-go/mcp"
)

// deviceResolver is the subset of *whatsapp.DeviceManager the MCP layer needs.
type deviceResolver interface {
	ResolveDevice(deviceID string) (*whatsapp.DeviceInstance, string, error)
}

// resolveDeviceContext returns a context carrying the device a tool call acts
// on. Priority: explicit device_id argument > device injected by the HTTP
// layer from the X-Device-Id header (see route.go) > error. Mirrors REST's
// DeviceMiddleware semantics for MCP handlers.
func resolveDeviceContext(ctx context.Context, request mcpg.CallToolRequest, resolver deviceResolver) (context.Context, *whatsapp.DeviceInstance, error) {
	if deviceID := strings.TrimSpace(request.GetString("device_id", "")); deviceID != "" {
		if resolver == nil {
			return ctx, nil, errors.New("device manager not initialized")
		}
		inst, _, err := resolver.ResolveDevice(deviceID)
		if err != nil {
			return ctx, nil, err
		}
		return whatsapp.ContextWithDevice(ctx, inst), inst, nil
	}
	if inst, ok := whatsapp.DeviceFromContext(ctx); ok && inst != nil {
		return ctx, inst, nil
	}
	return ctx, nil, errors.New("device identification required: set the X-Device-Id header or pass device_id")
}
