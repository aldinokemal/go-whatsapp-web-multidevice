package whatsapp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types/events"
)

type deliveredWebhook struct {
	url  string
	body map[string]any
}

// captureDeviceWebhooks sends webhooks to a stub instead of the network, with one global
// URL, no per-device config and no session ids, and restores the globals afterwards.
func captureDeviceWebhooks(t *testing.T) <-chan deliveredWebhook {
	t.Helper()
	origURLs, origEvents := config.WhatsappWebhook, config.WhatsappWebhookEvents
	origSubmit, origStorage, origSessionID := submitWebhookFn, webhookStorageForTest, sessionIDForJIDFn
	t.Cleanup(func() {
		config.WhatsappWebhook, config.WhatsappWebhookEvents = origURLs, origEvents
		submitWebhookFn, webhookStorageForTest, sessionIDForJIDFn = origSubmit, origStorage, origSessionID
	})

	config.WhatsappWebhook = []string{"https://global.example.test/webhook"}
	config.WhatsappWebhookEvents = nil
	webhookStorageForTest = func(string) (*domainChatStorage.DeviceRecord, error) { return nil, nil }
	sessionIDForJIDFn = func(string) string { return "" }

	got := make(chan deliveredWebhook, 4)
	submitWebhookFn = func(_ context.Context, payload map[string]any, url string, _ *domainChatStorage.DeviceWebhookConfig) error {
		got <- deliveredWebhook{url: url, body: payload}
		return nil
	}
	return got
}

func recvWebhook(t *testing.T, got <-chan deliveredWebhook) deliveredWebhook {
	t.Helper()
	select {
	case delivered := <-got:
		return delivered
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook submission")
		return deliveredWebhook{}
	}
}

func TestTemporaryBanPayload(t *testing.T) {
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		evt  events.TemporaryBan
		want map[string]any
	}{
		{
			name: "known reason with expiry",
			evt:  events.TemporaryBan{Code: events.TempBanSentToTooManyPeople, Expire: 2 * time.Hour},
			want: map[string]any{
				"code":               101,
				"reason":             "you sent too many messages to people who don't have you in their address books",
				"expires_in_seconds": int64(7200),
				"expires_at":         "2026-10-28T11:00:00Z",
			},
		},
		{
			name: "no expiry given",
			evt:  events.TemporaryBan{Code: events.TempBanBlockedByUsers},
			want: map[string]any{
				"code":               102,
				"reason":             "too many people blocked you",
				"expires_in_seconds": int64(0),
			},
		},
		{
			name: "unknown reason code",
			evt:  events.TemporaryBan{Code: 105, Expire: 90 * time.Second},
			want: map[string]any{
				"code":               105,
				"reason":             "you may have violated the terms of service (unknown error)",
				"expires_in_seconds": int64(90),
				"expires_at":         "2026-10-28T09:01:30Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, temporaryBanPayload(&tt.evt, now))
		})
	}
}

func TestReachoutTimelockPayload(t *testing.T) {
	active := &events.NotifyAccountReachoutTimelock{EnforcementType: "example-type", IsActive: true}
	active.TimeEnforcementEnds.Time = time.Date(2026, 10, 29, 6, 30, 0, 0, time.FixedZone("AST", 3*60*60))
	assert.Equal(t, map[string]any{
		"is_active":        true,
		"enforcement_type": "example-type",
		"ends_at":          "2026-10-29T03:30:00Z",
	}, reachoutTimelockPayload(active))

	lifted := &events.NotifyAccountReachoutTimelock{}
	assert.Equal(t, map[string]any{"is_active": false}, reachoutTimelockPayload(lifted))
}

func TestLoggedOutPayload(t *testing.T) {
	assert.Equal(t, map[string]any{
		"code":       406,
		"reason":     "logged out for unknown reason",
		"on_connect": true,
	}, loggedOutPayload(&events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureUnknownLogout}))

	assert.Equal(t, map[string]any{
		"code":       401,
		"reason":     "logged out from another device",
		"on_connect": false,
	}, loggedOutPayload(&events.LoggedOut{Reason: events.ConnectFailureLoggedOut}))
}

func TestHandlerTemporaryBanBroadcastsAndForwards(t *testing.T) {
	got := captureDeviceWebhooks(t)
	inst := &DeviceInstance{id: "slot-ban", jid: "6281111111111@s.whatsapp.net", createdAt: time.Now()}

	go handler(context.Background(), inst, &events.TemporaryBan{Code: events.TempBanBlockedByUsers, Expire: time.Hour})

	msg := recvBroadcast(t)
	assert.Equal(t, "DEVICE_TEMPORARY_BAN", msg.Code)
	result, ok := msg.Result.(map[string]any)
	require.True(t, ok, "broadcast result = %#v", msg.Result)
	assert.Equal(t, "slot-ban", result["device_id"])
	assert.Equal(t, 102, result["code"])

	delivered := recvWebhook(t, got)
	assert.Equal(t, EventTypeDeviceTemporaryBan, delivered.body["event"])
	assert.Equal(t, "6281111111111@s.whatsapp.net", delivered.body["device_id"])
	payload, ok := delivered.body["payload"].(map[string]any)
	require.True(t, ok, "webhook payload = %#v", delivered.body["payload"])
	assert.Equal(t, 102, payload["code"])
	assert.Equal(t, "too many people blocked you", payload["reason"])
	assert.Equal(t, int64(3600), payload["expires_in_seconds"])
}

func TestHandlerReachoutTimelockBroadcastsAndForwards(t *testing.T) {
	got := captureDeviceWebhooks(t)
	inst := &DeviceInstance{id: "slot-timelock", jid: "6283333333333@s.whatsapp.net", createdAt: time.Now()}
	evt := &events.NotifyAccountReachoutTimelock{EnforcementType: "example-type", IsActive: true}
	evt.TimeEnforcementEnds.Time = time.Date(2026, 10, 29, 3, 30, 0, 0, time.UTC)

	go handler(context.Background(), inst, evt)

	msg := recvBroadcast(t)
	assert.Equal(t, "DEVICE_REACHOUT_TIMELOCK", msg.Code)
	assert.Contains(t, msg.Message, "until 2026-10-29T03:30:00Z")

	delivered := recvWebhook(t, got)
	assert.Equal(t, EventTypeDeviceReachoutTimelock, delivered.body["event"])
	assert.Equal(t, "6283333333333@s.whatsapp.net", delivered.body["device_id"])
	assert.Equal(t, map[string]any{
		"is_active":        true,
		"enforcement_type": "example-type",
		"ends_at":          "2026-10-29T03:30:00Z",
	}, delivered.body["payload"])
}

// The keep-slot reset that a remote logout triggers clears the device's JID, and the
// webhook config and session id are found by that JID. The logout webhook must still
// reach a device-only webhook, addressed to the JID the device had.
func TestHandlerLoggedOutForwardsWithIdentityFromBeforeReset(t *testing.T) {
	got := captureDeviceWebhooks(t)
	config.WhatsappWebhook = nil

	const jid = "6282222222222@s.whatsapp.net"
	deviceURL := "https://device.example.test/webhook"
	var reset atomic.Bool
	webhookStorageForTest = func(deviceJID string) (*domainChatStorage.DeviceRecord, error) {
		if reset.Load() || deviceJID != jid {
			return nil, nil
		}
		return &domainChatStorage.DeviceRecord{DeviceID: "slot-out", JID: jid, WebhookURL: &deviceURL}, nil
	}

	inst := &DeviceInstance{id: "slot-out", jid: jid, createdAt: time.Now()}
	sessionIDForJIDFn = func(deviceJID string) string {
		if deviceJID != "" && deviceJID == inst.JID() {
			return inst.ID()
		}
		return ""
	}
	// Stands in for the device manager's keep-slot callback.
	inst.SetOnLoggedOut(func(string) {
		reset.Store(true)
		inst.ResetClient()
	})

	go handler(context.Background(), inst, &events.LoggedOut{OnConnect: true, Reason: events.ConnectFailureUnknownLogout})

	msg := recvBroadcast(t)
	assert.Equal(t, "DEVICE_LOGGED_OUT", msg.Code)
	assert.Equal(t, map[string]string{"device_id": "slot-out", "reason": "406: logged out for unknown reason"}, msg.Result)

	delivered := recvWebhook(t, got)
	assert.Equal(t, deviceURL, delivered.url)
	assert.Equal(t, EventTypeDeviceLoggedOut, delivered.body["event"])
	assert.Equal(t, jid, delivered.body["device_id"])
	assert.Equal(t, "slot-out", delivered.body["session_id"])
	assert.Equal(t, map[string]any{
		"code":       406,
		"reason":     "logged out for unknown reason",
		"on_connect": true,
	}, delivered.body["payload"])

	assert.Empty(t, inst.JID(), "the keep-slot reset should still clear the JID")
}
