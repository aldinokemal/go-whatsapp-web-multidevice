package whatsapp

import (
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/stretchr/testify/assert"
)

func TestCallWebhookEventName(t *testing.T) {
	assert.Equal(t, "call.started", callWebhookEventName("CALL_STARTED", domainCall.CallInfo{}))
	assert.Equal(t, "call.incoming", callWebhookEventName("CALL_INCOMING", domainCall.CallInfo{}))
	assert.Equal(t, "call.accept", callWebhookEventName("CALL_STATE", domainCall.CallInfo{Status: domainCall.StatusConnecting}))
	assert.Equal(t, "call.active", callWebhookEventName("CALL_STATE", domainCall.CallInfo{Status: domainCall.StatusActive}))
	assert.Equal(t, "call.ended", callWebhookEventName("CALL_ENDED", domainCall.CallInfo{}))
}

func TestCallWebhookPayload(t *testing.T) {
	body := callWebhookPayload("call.ended", domainCall.CallInfo{
		DeviceID:  "device-a",
		CallID:    "call-1",
		PeerJID:   "5511999999999@s.whatsapp.net",
		Direction: domainCall.DirectionOutbound,
		Status:    domainCall.StatusEnded,
		MediaType: domainCall.MediaTypeAudio,
		EndReason: "user_ended",
	})

	assert.Equal(t, "call.ended", body["event"])
	assert.Equal(t, "device-a", body["device_id"])
	payload := body["payload"].(map[string]any)
	assert.Equal(t, "call-1", payload["call_id"])
	assert.Equal(t, "user_ended", payload["end_reason"])
}
