package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestIsSecretEncryptedEdit(t *testing.T) {
	assert.False(t, IsSecretEncryptedEdit(nil))
	assert.False(t, IsSecretEncryptedEdit(&waE2E.Message{
		Conversation: protoString("hello"),
	}))

	assert.True(t, IsSecretEncryptedEdit(&waE2E.Message{
		SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
			SecretEncType: waE2E.SecretEncryptedMessage_MESSAGE_EDIT.Enum(),
		},
	}))
}

func TestExtractMessageEdit(t *testing.T) {
	assert.Nil(t, ExtractMessageEdit(nil))
	assert.Nil(t, ExtractMessageEdit(&waE2E.Message{Conversation: protoString("x")}))

	edit := ExtractMessageEdit(&waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key: &waCommon.MessageKey{
				ID: protoString("ORIG-1"),
			},
			EditedMessage: &waE2E.Message{
				Conversation: protoString("updated text"),
			},
		},
	})
	require.NotNil(t, edit)
	assert.Equal(t, "ORIG-1", edit.OriginalMessageID)
	assert.Equal(t, "updated text", ExtractEditBody(edit.Edited))
}

func TestResolveIncomingMessagePlainEdit(t *testing.T) {
	evt := &events.Message{
		Info: types.MessageInfo{ID: "EVT-1"},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
				Key:  &waCommon.MessageKey{ID: protoString("ORIG-2")},
				EditedMessage: &waE2E.Message{
					ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text: protoString("extended update"),
					},
				},
			},
		},
	}

	resolved, err := ResolveIncomingMessage(context.Background(), nil, evt)
	require.NoError(t, err)
	edit := ExtractMessageEdit(resolved)
	require.NotNil(t, edit)
	assert.Equal(t, "extended update", ExtractEditBody(edit.Edited))
}

func TestResolveIncomingMessageSecretEditNoClient(t *testing.T) {
	evt := &events.Message{
		Info: types.MessageInfo{ID: "EVT-SEC"},
		Message: &waE2E.Message{
			SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
				SecretEncType: waE2E.SecretEncryptedMessage_MESSAGE_EDIT.Enum(),
			},
		},
	}

	_, err := ResolveIncomingMessage(context.Background(), nil, evt)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSecretEditDecrypt)
}

func TestBuildEventPayloadMessageEdited(t *testing.T) {
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("456", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "EDIT-EVT",
			PushName:  "Alice",
			Timestamp: time.Date(2026, time.May, 29, 4, 58, 11, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
				Key: &waCommon.MessageKey{
					ID: protoString("ORIG-MSG"),
				},
				EditedMessage: &waE2E.Message{
					Conversation: protoString("new body text"),
				},
			},
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	require.NoError(t, err)
	assert.Equal(t, EventTypeMessageEdited, eventType)
	assert.Equal(t, "ORIG-MSG", payload["original_message_id"])
	assert.Equal(t, "new body text", payload["body"])
}
