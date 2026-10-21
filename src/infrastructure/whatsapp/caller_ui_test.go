package whatsapp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIncomingCallUIBroadcastFromPayload(t *testing.T) {
	payload := map[string]any{
		"device_id": "628123456789@s.whatsapp.net",
		"payload": map[string]any{
			"call_id":  "CALL1",
			"from":     "628999999999@s.whatsapp.net",
			"from_lid": "998877@s.whatsapp.net",
		},
	}

	notification := incomingCallUIBroadcastFromPayload(payload, "sales-slot")

	require.Equal(t, IncomingCallUIBroadcast{
		Event:     IncomingCallEventRinging,
		CallID:    "CALL1",
		From:      "628999999999@s.whatsapp.net",
		FromLID:   "998877@s.whatsapp.net",
		DeviceID:  "sales-slot",
		DeviceJID: "628123456789@s.whatsapp.net",
	}, notification)
}

func TestIncomingCallUIBroadcastFromPayloadToleratesMissingBody(t *testing.T) {
	notification := incomingCallUIBroadcastFromPayload(map[string]any{}, "sales-slot")

	require.Equal(t, IncomingCallUIBroadcast{
		Event:    IncomingCallEventRinging,
		DeviceID: "sales-slot",
	}, notification)
}

func TestNotifyIncomingCallUIDeliversAndRecovers(t *testing.T) {
	t.Cleanup(func() { SetIncomingCallUIListener(nil) })

	var got []IncomingCallUIBroadcast
	SetIncomingCallUIListener(func(notification IncomingCallUIBroadcast) {
		got = append(got, notification)
	})
	notifyIncomingCallUI(IncomingCallUIBroadcast{CallID: "CALL1"})

	require.Len(t, got, 1)
	require.Equal(t, "CALL1", got[0].CallID)

	// A buggy dashboard bridge must not break the call receive path.
	SetIncomingCallUIListener(func(IncomingCallUIBroadcast) { panic("dashboard boom") })
	require.NotPanics(t, func() { notifyIncomingCallUI(IncomingCallUIBroadcast{}) })

	SetIncomingCallUIListener(nil)
	require.NotPanics(t, func() { notifyIncomingCallUI(IncomingCallUIBroadcast{}) })
}
