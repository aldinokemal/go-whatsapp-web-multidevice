package whatsapp

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestBuildEventPayloadIncludesIsFromMe(t *testing.T) {
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("123", types.DefaultUserServer),
				IsFromMe: true,
			},
			ID:        "MSG123",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			Conversation: protoString("hello"),
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if eventType != EventTypeMessage {
		t.Fatalf("expected event type %s, got %s", EventTypeMessage, eventType)
	}
	if value, ok := payload["is_from_me"]; !ok {
		t.Fatalf("expected is_from_me in payload")
	} else if isFromMe, ok := value.(bool); !ok || !isFromMe {
		t.Fatalf("expected is_from_me=true, got %v", value)
	}
}

func TestBuildEventPayloadRevokedIncludesIsFromMe(t *testing.T) {
	key := &waCommon.MessageKey{
		RemoteJID: protoString("123@s.whatsapp.net"),
		FromMe:    protoBool(true),
		ID:        protoString("REV123"),
	}
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("123", types.DefaultUserServer),
				IsFromMe: true,
			},
			ID:        "MSG124",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: protoProtocolMessageType(waE2E.ProtocolMessage_REVOKE),
				Key:  key,
			},
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if eventType != EventTypeMessageRevoked {
		t.Fatalf("expected event type %s, got %s", EventTypeMessageRevoked, eventType)
	}
	if value, ok := payload["is_from_me"]; !ok {
		t.Fatalf("expected is_from_me in payload")
	} else if isFromMe, ok := value.(bool); !ok || !isFromMe {
		t.Fatalf("expected is_from_me=true, got %v", value)
	}
}

func protoString(value string) *string {
	return &value
}

func protoBool(value bool) *bool {
	return &value
}

func protoProtocolMessageType(value waE2E.ProtocolMessage_Type) *waE2E.ProtocolMessage_Type {
	return &value
}
