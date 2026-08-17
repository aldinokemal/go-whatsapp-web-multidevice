package whatsapp

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// A view-once "unavailable" placeholder (WhatsApp withholds view-once content
// from linked devices) must surface as a `message` event with view_once and
// unavailable flags and the regular from/chat_id fields, so consumers can
// render the same notice WhatsApp Web shows.
func TestBuildViewOncePlaceholderEvent(t *testing.T) {
	ts := time.Date(2023, 10, 15, 11, 41, 0, 0, time.UTC)
	evt := &events.UndecryptableMessage{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("628123456789", types.DefaultUserServer),
				Sender:   types.NewJID("628123456789", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "3EB0C127D7BACC83D6B9",
			Timestamp: ts,
			PushName:  "John Doe",
		},
		IsUnavailable:   true,
		UnavailableType: events.UnavailableTypeViewOnce,
	}

	envelope := buildViewOncePlaceholderEvent(context.Background(), nil, evt)

	if envelope["event"] != EventTypeMessage {
		t.Fatalf("event = %v, want %q", envelope["event"], EventTypeMessage)
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want map[string]any", envelope["payload"])
	}

	want := map[string]any{
		"id":          "3EB0C127D7BACC83D6B9",
		"timestamp":   ts.Format(time.RFC3339),
		"is_from_me":  false,
		"view_once":   true,
		"unavailable": true,
		"chat_id":     "628123456789@s.whatsapp.net",
		"from":        "628123456789@s.whatsapp.net",
		"from_name":   "John Doe",
	}
	for key, expected := range want {
		if payload[key] != expected {
			t.Errorf("payload[%q] = %v, want %v", key, payload[key], expected)
		}
	}
	if _, hasBody := payload["body"]; hasBody {
		t.Errorf("payload must not carry a body: the content was never delivered")
	}
}
