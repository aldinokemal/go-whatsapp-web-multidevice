package whatsapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
)

// ErrSecretEditDecrypt is returned when a SecretEncryptedMessage(MESSAGE_EDIT) cannot be decrypted.
var ErrSecretEditDecrypt = errors.New("failed to decrypt secret encrypted message edit")

// MessageEdit holds a parsed MESSAGE_EDIT protocol payload from a resolved (possibly decrypted) message.
type MessageEdit struct {
	Protocol          *waE2E.ProtocolMessage
	Edited            *waE2E.Message
	OriginalMessageID string
}

// IsSecretEncryptedEdit reports whether msg (after unwrap) is a SecretEncryptedMessage with MESSAGE_EDIT.
func IsSecretEncryptedEdit(msg *waE2E.Message) bool {
	if msg == nil {
		return false
	}
	unwrapped := utils.UnwrapMessage(msg)
	sem := unwrapped.GetSecretEncryptedMessage()
	return sem != nil && sem.GetSecretEncType() == waE2E.SecretEncryptedMessage_MESSAGE_EDIT
}

// ResolveIncomingMessage unwraps evt.Message and decrypts SecretEncryptedMessage(MESSAGE_EDIT) when present.
func ResolveIncomingMessage(ctx context.Context, client *whatsmeow.Client, evt *events.Message) (*waE2E.Message, error) {
	if evt == nil || evt.Message == nil {
		return nil, nil
	}

	msg := utils.UnwrapMessage(evt.Message)

	sem := msg.GetSecretEncryptedMessage()
	if sem == nil || sem.GetSecretEncType() != waE2E.SecretEncryptedMessage_MESSAGE_EDIT {
		return msg, nil
	}

	if client == nil {
		logrus.Errorf("SecretEncryptedMessage(MESSAGE_EDIT) for %s requires a connected client", evt.Info.ID)
		return nil, fmt.Errorf("%w: no client", ErrSecretEditDecrypt)
	}

	decrypted, err := client.DecryptSecretEncryptedMessage(ctx, evt)
	if err != nil {
		logrus.Errorf("Failed to decrypt SecretEncryptedMessage(MESSAGE_EDIT) for %s: %v", evt.Info.ID, err)
		return nil, fmt.Errorf("%w: %v", ErrSecretEditDecrypt, err)
	}
	if decrypted == nil {
		logrus.Errorf("DecryptSecretEncryptedMessage returned nil for %s", evt.Info.ID)
		return nil, fmt.Errorf("%w: nil decrypted message", ErrSecretEditDecrypt)
	}

	return utils.UnwrapMessage(decrypted), nil
}

// ExtractMessageEdit returns edit metadata when msg contains ProtocolMessage MESSAGE_EDIT.
func ExtractMessageEdit(msg *waE2E.Message) *MessageEdit {
	if msg == nil {
		return nil
	}
	protocolMessage := msg.GetProtocolMessage()
	if protocolMessage == nil || protocolMessage.GetType() != waE2E.ProtocolMessage_MESSAGE_EDIT {
		return nil
	}
	editedMessage := protocolMessage.GetEditedMessage()
	if editedMessage == nil {
		return nil
	}

	originalMessageID := ""
	if key := protocolMessage.GetKey(); key != nil {
		originalMessageID = key.GetID()
	}

	return &MessageEdit{
		Protocol:          protocolMessage,
		Edited:            editedMessage,
		OriginalMessageID: originalMessageID,
	}
}

// ExtractEditBody returns the new text content from an edited message proto.
func ExtractEditBody(edited *waE2E.Message) string {
	return utils.ExtractMessageTextFromProto(edited)
}
