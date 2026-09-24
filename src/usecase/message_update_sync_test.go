package usecase

import (
	"errors"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An edit sent through the API must land in chat storage, exactly as an edit
// that arrives from another device already does.
//
// Chat storage is what mergeReplyContext quotes from, so an unsynced edit means
// every later reply to that message quotes the recipient the pre-edit text.
func TestUpdateMessageSyncsEditToChatStorage(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "edited content",
	})
	require.NoError(t, err)

	stored, err := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", "message-1")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "edited content", stored.Content)
}

// The edit history row is what makes an edit auditable after the fact, and the
// inbound path already writes one — the API path must not be the odd one out.
func TestUpdateMessageRecordsEditHistory(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "edited content",
	})
	require.NoError(t, err)

	edits, err := repo.GetMessageEdits("message-1", "device-a@s.whatsapp.net")
	require.NoError(t, err)
	require.Len(t, edits, 1)
	assert.Equal(t, "stored content", edits[0].PreviousContent)
	assert.Equal(t, "edited content", edits[0].NewContent)
}

// Only the device that owns the conversation is touched. Message IDs are unique
// per device row, so a sibling device's copy must be left exactly as it was.
func TestUpdateMessageLeavesOtherDevicesAlone(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "edited content",
	})
	require.NoError(t, err)

	other, err := repo.GetMessageByIDAndDevice("device-b@s.whatsapp.net", "message-1")
	require.NoError(t, err)
	require.NotNil(t, other)
	assert.Equal(t, "stored content", other.Content)
}

// Mirrors TestMessageActionsKeepStoredMessageWhenWhatsAppFails: nothing local
// changes when the edit never reached WhatsApp, so storage cannot drift ahead of
// what the recipient actually has.
func TestUpdateMessageKeepsStoredContentWhenWhatsAppFails(t *testing.T) {
	remoteErr := errors.New("whatsapp unavailable")
	service, repo, ctx := newMessageActionTestService(t, remoteErr)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "edited content",
	})
	require.ErrorIs(t, err, remoteErr)

	stored, lookupErr := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", "message-1")
	require.NoError(t, lookupErr)
	require.NotNil(t, stored)
	assert.Equal(t, "stored content", stored.Content)

	edits, lookupErr := repo.GetMessageEdits("message-1", "device-a@s.whatsapp.net")
	require.NoError(t, lookupErr)
	assert.Empty(t, edits)
}
