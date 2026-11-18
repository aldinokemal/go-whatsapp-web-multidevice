package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type capturedWebhook struct {
	event   string
	payload map[string]any
}

// captureGroupWebhooks routes every webhook submission into the returned slice and
// restores the replaced config and package globals when the test ends.
func captureGroupWebhooks(t *testing.T) *[]capturedWebhook {
	t.Helper()

	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	originalIgnore := config.WhatsappWebhookIgnoreJids
	originalChatwoot := config.ChatwootEnabled
	originalSubmit := submitWebhookFn
	originalStorage := webhookStorageForTest
	originalSessionID := sessionIDForJIDFn
	t.Cleanup(func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
		config.WhatsappWebhookIgnoreJids = originalIgnore
		config.ChatwootEnabled = originalChatwoot
		submitWebhookFn = originalSubmit
		webhookStorageForTest = originalStorage
		sessionIDForJIDFn = originalSessionID
	})

	config.WhatsappWebhook = []string{"https://example.test/webhook"}
	config.WhatsappWebhookEvents = nil
	config.WhatsappWebhookIgnoreJids = nil
	config.ChatwootEnabled = false
	webhookStorageForTest = func(string) (*chatstorage.DeviceRecord, error) { return nil, nil }
	sessionIDForJIDFn = func(string) string { return "" }

	var captured []capturedWebhook
	submitWebhookFn = func(_ context.Context, payload map[string]any, _ string, _ *chatstorage.DeviceWebhookConfig) error {
		event, _ := payload["event"].(string)
		inner, _ := payload["payload"].(map[string]any)
		captured = append(captured, capturedWebhook{event: event, payload: inner})
		return nil
	}
	return &captured
}

// groupNameEvent builds a GroupInfo event that carries only a group name change.
func groupNameEvent(name types.GroupName) *events.GroupInfo {
	return &events.GroupInfo{
		JID:       types.NewJID("120363402106000000", types.GroupServer),
		Timestamp: time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC),
		Name:      &name,
	}
}

func TestForwardGroupInfoToWebhook_NameChange(t *testing.T) {
	captured := captureGroupWebhooks(t)

	evt := groupNameEvent(types.GroupName{
		Name:        "Kitchen @ Home",
		NameSetBy:   types.NewJID("123456789012345", types.HiddenUserServer),
		NameSetByPN: types.NewJID("6289685000000", types.DefaultUserServer),
	})

	if err := forwardGroupInfoToWebhook(context.Background(), evt, "628123456789@s.whatsapp.net", nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(*captured) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(*captured))
	}
	got := (*captured)[0]
	if got.event != "group.info" {
		t.Errorf("expected event group.info, got %q", got.event)
	}
	if got.payload["chat_id"] != "120363402106000000@g.us" {
		t.Errorf("unexpected chat_id %v", got.payload["chat_id"])
	}
	if got.payload["name"] != "Kitchen @ Home" {
		t.Errorf("unexpected name %v", got.payload["name"])
	}
	if got.payload["name_set_by"] != "6289685000000@s.whatsapp.net" {
		t.Errorf("expected the phone number JID in name_set_by, got %v", got.payload["name_set_by"])
	}
}

func TestCreateGroupNamePayload(t *testing.T) {
	evt := groupNameEvent(types.GroupName{Name: "Renamed"})

	body := createGroupNamePayload(context.Background(), evt, "628123456789@s.whatsapp.net", nil)

	if body["event"] != "group.info" {
		t.Errorf("expected event group.info, got %v", body["event"])
	}
	if body["device_id"] != "628123456789@s.whatsapp.net" {
		t.Errorf("unexpected device_id %v", body["device_id"])
	}
	if body["timestamp"] != "2026-10-14T09:30:00Z" {
		t.Errorf("unexpected timestamp %v", body["timestamp"])
	}
	payload := body["payload"].(map[string]any)
	if _, ok := payload["name_set_by"]; ok {
		t.Errorf("expected no name_set_by when WhatsApp does not say who set the name, got %v", payload["name_set_by"])
	}
}

func TestGroupNameSetBy(t *testing.T) {
	phone := types.NewJID("6289685000000", types.DefaultUserServer)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)

	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	tests := []struct {
		name string
		in   types.GroupName
		want string
	}{
		{"phone number JID next to a LID", types.GroupName{NameSetBy: lid, NameSetByPN: phone}, "6289685000000@s.whatsapp.net"},
		{"phone number JID only", types.GroupName{NameSetBy: phone}, "6289685000000@s.whatsapp.net"},
		{"unresolvable LID only", types.GroupName{NameSetBy: lid}, "123456789012345@lid"},
		{"no setter", types.GroupName{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := groupNameSetBy(context.Background(), &tt.in, nil); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestForwardGroupInfoToWebhook_ParticipantsOnlySendsNoGroupInfo(t *testing.T) {
	captured := captureGroupWebhooks(t)

	evt := &events.GroupInfo{
		JID:       types.NewJID("120363402106000000", types.GroupServer),
		Timestamp: time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC),
		Join:      []types.JID{types.NewJID("6289685000000", types.DefaultUserServer)},
	}

	if err := forwardGroupInfoToWebhook(context.Background(), evt, "", nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(*captured) != 1 || (*captured)[0].event != "group.participants" {
		t.Fatalf("expected only a group.participants webhook, got %+v", *captured)
	}
}

func TestForwardGroupInfoToWebhook_NameAndParticipantsSendsBoth(t *testing.T) {
	captured := captureGroupWebhooks(t)

	evt := groupNameEvent(types.GroupName{Name: "Renamed"})
	evt.Join = []types.JID{types.NewJID("6289685000000", types.DefaultUserServer)}

	if err := forwardGroupInfoToWebhook(context.Background(), evt, "", nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	counts := map[string]int{}
	for _, webhook := range *captured {
		counts[webhook.event]++
	}
	if len(*captured) != 2 || counts["group.participants"] != 1 || counts["group.info"] != 1 {
		t.Fatalf("expected one group.participants and one group.info webhook, got %+v", *captured)
	}
}

func TestForwardGroupInfoToWebhook_NameChangeFollowsEventWhitelist(t *testing.T) {
	tests := []struct {
		name      string
		whitelist []string
		want      int
	}{
		{"group.info whitelisted", []string{"group.info"}, 1},
		{"only group.participants whitelisted", []string{"group.participants"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captured := captureGroupWebhooks(t)
			config.WhatsappWebhookEvents = tt.whitelist

			evt := groupNameEvent(types.GroupName{Name: "Renamed"})

			if err := forwardGroupInfoToWebhook(context.Background(), evt, "", nil); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if len(*captured) != tt.want {
				t.Fatalf("expected %d webhook(s), got %+v", tt.want, *captured)
			}
		})
	}
}
