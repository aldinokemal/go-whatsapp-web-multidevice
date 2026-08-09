package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubResolver struct {
	inst  *whatsapp.DeviceInstance
	gotID string
	err   error
}

func (s *stubResolver) ResolveDevice(deviceID string) (*whatsapp.DeviceInstance, string, error) {
	s.gotID = deviceID
	return s.inst, deviceID, s.err
}

func callReq(args map[string]any) mcpg.CallToolRequest {
	req := mcpg.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

func TestResolveDeviceContext(t *testing.T) {
	ctxDevice := &whatsapp.DeviceInstance{}
	argDevice := &whatsapp.DeviceInstance{}

	t.Run("device_id arg overrides context device", func(t *testing.T) {
		r := &stubResolver{inst: argDevice}
		ctx := whatsapp.ContextWithDevice(context.Background(), ctxDevice)
		newCtx, inst, err := resolveDeviceContext(ctx, callReq(map[string]any{"device_id": "dev2"}), r)
		require.NoError(t, err)
		assert.Same(t, argDevice, inst)
		assert.Equal(t, "dev2", r.gotID)
		got, ok := whatsapp.DeviceFromContext(newCtx)
		require.True(t, ok)
		assert.Same(t, argDevice, got)
	})

	t.Run("falls back to context device", func(t *testing.T) {
		ctx := whatsapp.ContextWithDevice(context.Background(), ctxDevice)
		_, inst, err := resolveDeviceContext(ctx, callReq(nil), &stubResolver{})
		require.NoError(t, err)
		assert.Same(t, ctxDevice, inst)
	})

	t.Run("resolver error surfaces", func(t *testing.T) {
		r := &stubResolver{err: errors.New("no such device")}
		_, _, err := resolveDeviceContext(context.Background(), callReq(map[string]any{"device_id": "ghost"}), r)
		require.ErrorContains(t, err, "no such device")
	})

	t.Run("no device anywhere errors", func(t *testing.T) {
		_, _, err := resolveDeviceContext(context.Background(), callReq(nil), &stubResolver{})
		require.ErrorContains(t, err, "device identification required")
	})

	t.Run("nil device stored in context errors", func(t *testing.T) {
		ctx := whatsapp.ContextWithDevice(context.Background(), nil)
		_, _, err := resolveDeviceContext(ctx, callReq(nil), &stubResolver{})
		require.ErrorContains(t, err, "device identification required")
	})
}
