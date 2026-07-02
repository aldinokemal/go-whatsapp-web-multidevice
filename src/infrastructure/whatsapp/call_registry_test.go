package whatsapp

import (
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveCallRegistryScopesCallsByDevice(t *testing.T) {
	registry := NewActiveCallRegistry()

	first := &domainCall.CallInfo{DeviceID: "device-a", CallID: "same-id", PeerJID: "5511999999999@s.whatsapp.net"}
	second := &domainCall.CallInfo{DeviceID: "device-b", CallID: "same-id", PeerJID: "5521888888888@s.whatsapp.net"}

	require.NoError(t, registry.Upsert(first))
	require.NoError(t, registry.Upsert(second))

	gotFirst, ok := registry.Get("device-a", "same-id")
	require.True(t, ok)
	assert.Equal(t, "5511999999999@s.whatsapp.net", gotFirst.PeerJID)

	gotSecond, ok := registry.Get("device-b", "same-id")
	require.True(t, ok)
	assert.Equal(t, "5521888888888@s.whatsapp.net", gotSecond.PeerJID)

	assert.Len(t, registry.List("device-a"), 1)
	assert.Len(t, registry.List("device-b"), 1)
}

func TestActiveCallRegistryRemovesCallsByDevice(t *testing.T) {
	registry := NewActiveCallRegistry()

	require.NoError(t, registry.Upsert(&domainCall.CallInfo{DeviceID: "device-a", CallID: "call-1"}))
	require.NoError(t, registry.Upsert(&domainCall.CallInfo{DeviceID: "device-b", CallID: "call-1"}))

	removed, ok := registry.Remove("device-a", "call-1")
	require.True(t, ok)
	assert.Equal(t, "device-a", removed.DeviceID)

	_, ok = registry.Get("device-a", "call-1")
	assert.False(t, ok)

	_, ok = registry.Get("device-b", "call-1")
	assert.True(t, ok)
}

func TestActiveCallRegistryRejectsMissingKeys(t *testing.T) {
	registry := NewActiveCallRegistry()

	assert.Error(t, registry.Upsert(nil))
	assert.Error(t, registry.Upsert(&domainCall.CallInfo{CallID: "call-1"}))
	assert.Error(t, registry.Upsert(&domainCall.CallInfo{DeviceID: "device-a"}))
}
