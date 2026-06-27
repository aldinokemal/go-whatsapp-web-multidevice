package usecase

import (
	"context"
	"database/sql"
	"testing"
	"time"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCallRuntime struct {
	startPhone string
	calls      map[string]domainCall.CallInfo
}

func (f *fakeCallRuntime) StartCall(_ context.Context, device *whatsapp.DeviceInstance, phone string) (domainCall.CallInfo, error) {
	f.startPhone = phone
	info := domainCall.CallInfo{
		DeviceID:  device.ID(),
		CallID:    "call-1",
		PeerJID:   phone + "@s.whatsapp.net",
		Direction: domainCall.DirectionOutbound,
		Status:    domainCall.StatusRinging,
		MediaType: domainCall.MediaTypeAudio,
		StartedAt: time.Now(),
	}
	f.calls[info.CallID] = info
	return info, nil
}

func (f *fakeCallRuntime) AcceptCall(_ context.Context, deviceID, callID string) (domainCall.CallInfo, error) {
	info := f.calls[callID]
	info.DeviceID = deviceID
	info.Status = domainCall.StatusConnecting
	f.calls[callID] = info
	return info, nil
}

func (f *fakeCallRuntime) RejectCall(_ context.Context, deviceID, callID string) (domainCall.CallInfo, error) {
	info := f.calls[callID]
	info.DeviceID = deviceID
	info.Status = domainCall.StatusEnded
	info.EndReason = "declined"
	f.calls[callID] = info
	return info, nil
}

func (f *fakeCallRuntime) EndCall(_ context.Context, deviceID, callID string) (domainCall.CallInfo, error) {
	info := f.calls[callID]
	info.DeviceID = deviceID
	info.Status = domainCall.StatusEnded
	info.EndReason = "user_ended"
	f.calls[callID] = info
	return info, nil
}

func (f *fakeCallRuntime) ExchangeWebRTC(_ context.Context, _, callID, sdpOffer string) (string, error) {
	if _, ok := f.calls[callID]; ok && sdpOffer != "" {
		return "answer", nil
	}
	return "", nil
}

func (f *fakeCallRuntime) GetCall(_ context.Context, deviceID, callID string) (domainCall.CallInfo, bool) {
	info, ok := f.calls[callID]
	info.DeviceID = deviceID
	return info, ok
}

func (f *fakeCallRuntime) ListCalls(_ context.Context, deviceID string) []domainCall.CallInfo {
	result := make([]domainCall.CallInfo, 0, len(f.calls))
	for _, info := range f.calls {
		info.DeviceID = deviceID
		result = append(result, info)
	}
	return result
}

func newFakeCallService() (domainCall.ICallUsecase, *fakeCallRuntime, context.Context) {
	runtime := &fakeCallRuntime{calls: make(map[string]domainCall.CallInfo)}
	service := NewCallService(runtime)
	device := whatsapp.NewDeviceInstance("device-a", nil, nil)
	ctx := whatsapp.ContextWithDevice(context.Background(), device)
	return service, runtime, ctx
}

func TestCallServiceStartCallValidatesAndUsesDeviceContext(t *testing.T) {
	service, runtime, ctx := newFakeCallService()

	response, err := service.StartCall(ctx, domainCall.StartCallRequest{Phone: "5511999999999"})
	require.NoError(t, err)

	assert.Equal(t, "5511999999999", runtime.startPhone)
	assert.Equal(t, "device-a", response.Call.DeviceID)
	assert.Equal(t, domainCall.StatusRinging, response.Call.Status)
}

func TestCallServiceStartCallRequiresDeviceContext(t *testing.T) {
	service, _, _ := newFakeCallService()

	_, err := service.StartCall(context.Background(), domainCall.StartCallRequest{Phone: "5511999999999"})
	assert.Error(t, err)
}

func TestCallServiceExchangeWebRTC(t *testing.T) {
	service, _, ctx := newFakeCallService()
	_, err := service.StartCall(ctx, domainCall.StartCallRequest{Phone: "5511999999999"})
	require.NoError(t, err)

	response, err := service.ExchangeWebRTC(ctx, domainCall.WebRTCRequest{CallID: "call-1", SDPOffer: "v=0\r\n"})
	require.NoError(t, err)
	assert.Equal(t, "call-1", response.CallID)
	assert.Equal(t, "answer", response.SDPAnswer)
}

func TestCallServiceListsPersistedCallHistory(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())

	startedAt := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.StoreCallRecord(&domainChatStorage.CallRecord{
		DeviceID:  "device-a",
		CallID:    "persisted-call",
		PeerJID:   "5511999999999@s.whatsapp.net",
		Direction: string(domainCall.DirectionOutbound),
		Status:    string(domainCall.StatusEnded),
		MediaType: string(domainCall.MediaTypeAudio),
		StartedAt: startedAt,
		UpdatedAt: startedAt,
		EndReason: "completed",
		Metadata:  `{"source":"test"}`,
	}))

	runtime := &fakeCallRuntime{calls: make(map[string]domainCall.CallInfo)}
	service := NewCallService(runtime, repo)
	device := whatsapp.NewDeviceInstance("device-a", nil, nil)
	ctx := whatsapp.ContextWithDevice(context.Background(), device)

	response, err := service.ListCalls(ctx)
	require.NoError(t, err)

	require.Len(t, response.Data, 1)
	assert.Equal(t, "persisted-call", response.Data[0].CallID)
	assert.Equal(t, domainCall.StatusEnded, response.Data[0].Status)
	assert.Equal(t, "completed", response.Data[0].EndReason)
}
