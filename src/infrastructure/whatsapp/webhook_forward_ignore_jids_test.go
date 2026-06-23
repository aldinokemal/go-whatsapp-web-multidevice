package whatsapp

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// ignoreJidPayload builds a webhook envelope with the nested inner payload where chat_id/from
// live (matching how event_message.go etc. shape the body passed to the forwarder).
func ignoreJidPayload(chatID, from string) map[string]any {
	return map[string]any{
		"event":     "message",
		"device_id": "org_1",
		"payload": map[string]any{
			"chat_id": chatID,
			"from":    from,
		},
	}
}

// runIgnoreJidForward runs the forwarder with a single webhook configured and the given
// WHATSAPP_WEBHOOK_IGNORE_JIDS list, reporting whether the event was forwarded (submit called).
func runIgnoreJidForward(t *testing.T, ignoreJids []string, eventName string, payload map[string]any) bool {
	t.Helper()

	originalWebhooks := config.WhatsappWebhook
	originalEvents := config.WhatsappWebhookEvents
	originalIgnore := config.WhatsappWebhookIgnoreJids
	config.WhatsappWebhook = []string{"https://test.com"}
	config.WhatsappWebhookEvents = nil
	config.WhatsappWebhookIgnoreJids = ignoreJids
	defer func() {
		config.WhatsappWebhook = originalWebhooks
		config.WhatsappWebhookEvents = originalEvents
		config.WhatsappWebhookIgnoreJids = originalIgnore
	}()

	called := false
	originalSubmit := submitWebhookFn
	submitWebhookFn = func(context.Context, map[string]any, string) error {
		called = true
		return nil
	}
	defer func() { submitWebhookFn = originalSubmit }()

	if err := forwardPayloadToConfiguredWebhooks(context.Background(), payload, eventName); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	return called
}

func TestWebhookIgnoreJID_GroupWildcardDropsGroupMessage(t *testing.T) {
	payload := ignoreJidPayload("120363999000111@g.us", "628111@s.whatsapp.net")
	if runIgnoreJidForward(t, []string{"@g.us"}, "message", payload) {
		t.Fatal("group message should be dropped when @g.us is in WHATSAPP_WEBHOOK_IGNORE_JIDS")
	}
}

func TestWebhookIgnoreJID_GroupWildcardKeepsDirectMessage(t *testing.T) {
	payload := ignoreJidPayload("628111@s.whatsapp.net", "628111@s.whatsapp.net")
	if !runIgnoreJidForward(t, []string{"@g.us"}, "message", payload) {
		t.Fatal("1:1 message should still be forwarded when only @g.us is ignored")
	}
}

func TestWebhookIgnoreJID_ExactJIDMatchesSender(t *testing.T) {
	payload := ignoreJidPayload("628111@s.whatsapp.net", "628999@s.whatsapp.net")
	if runIgnoreJidForward(t, []string{"628999@s.whatsapp.net"}, "message", payload) {
		t.Fatal("message should be dropped when its sender JID (from) is in the ignore list")
	}
}

func TestWebhookIgnoreJID_EmptyListForwardsAll(t *testing.T) {
	payload := ignoreJidPayload("120363999000111@g.us", "628111@s.whatsapp.net")
	if !runIgnoreJidForward(t, nil, "message", payload) {
		t.Fatal("group message should be forwarded when the ignore list is empty (default)")
	}
}

func TestWebhookIgnoreJID_EventWithoutInnerPayloadForwards(t *testing.T) {
	// Envelope with no nested "payload" map (no JID to match) must keep forwarding —
	// the filter must never drop events that carry no chat/sender JID.
	payload := map[string]any{"event": "qr", "device_id": "org_1", "code": "abc"}
	if !runIgnoreJidForward(t, []string{"@g.us"}, "qr", payload) {
		t.Fatal("event without an inner JID must be forwarded (defensive default)")
	}
}
