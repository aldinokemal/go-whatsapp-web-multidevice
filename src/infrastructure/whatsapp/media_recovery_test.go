package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func TestMediaRetryMessageInfoDirectChat(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000AA",
		ChatJID: "5511888888888@s.whatsapp.net",
		Sender:  "5511888888888@s.whatsapp.net",
	})

	require.NoError(t, err)
	require.Equal(t, types.MessageID("3A0000000000000000AA"), info.ID)
	require.Equal(t, types.NewJID("5511888888888", types.DefaultUserServer), info.Chat)
	require.False(t, info.IsGroup)
	require.False(t, info.IsFromMe)
	require.True(t, info.Sender.IsEmpty(), "direct chats do not send a participant")
}

func TestMediaRetryMessageInfoGroupCarriesSender(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:       "3A0000000000000000BB",
		ChatJID:  "120363000000000001@g.us",
		Sender:   "123456789012345:7@lid",
		IsFromMe: true,
	})

	require.NoError(t, err)
	require.True(t, info.IsGroup)
	require.True(t, info.IsFromMe)
	require.Equal(t, types.NewJID("123456789012345", types.HiddenUserServer), info.Sender)
}

func TestMediaRetryMessageInfoStatusCarriesAuthor(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000DD",
		ChatJID: "status@broadcast",
		Sender:  "5511888888888@s.whatsapp.net",
	})

	require.NoError(t, err)
	require.True(t, info.IsGroup, "whatsmeow sends the participant for broadcast messages only when IsGroup is set")
	require.Equal(t, types.NewJID("5511888888888", types.DefaultUserServer), info.Sender)
}

func TestMediaRetryMessageInfoGroupWithoutSenderFails(t *testing.T) {
	_, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000CC",
		ChatJID: "120363000000000001@g.us",
	})

	require.ErrorContains(t, err, "no valid sender")
}

func TestMediaRetryMessageInfoInvalidChatFails(t *testing.T) {
	_, err := mediaRetryMessageInfo(&domainChatStorage.Message{ID: "X", ChatJID: ""})

	require.ErrorContains(t, err, "invalid chat JID")
}

func TestRecoverKeepsOriginalErrorForMessageThePhoneCannotBeAskedAbout(t *testing.T) {
	recovery := ExpiredMediaRecovery{
		RequestReupload: func(context.Context, *whatsmeow.Client, types.MessageInfo, []byte, time.Duration) (string, error) {
			t.Error("a message without a valid identity must not reach the phone")
			return "", nil
		},
	}
	groupMessageWithoutSender := &domainChatStorage.Message{ID: "no-sender", ChatJID: "120363000000000001@g.us", MediaType: "image"}

	_, err := recovery.Recover(context.Background(), nil, groupMessageWithoutSender, t.TempDir(), whatsmeow.ErrMediaDownloadFailedWith404)

	require.ErrorContains(t, err, "no valid sender")
	require.ErrorIs(t, err, whatsmeow.ErrMediaDownloadFailedWith404)
}

// bulkRecoveryPhone plays the phone for a run over several expired messages.
type bulkRecoveryPhone struct {
	requestErr error
	requests   []types.MessageID
}

func (p *bulkRecoveryPhone) recovery() *BulkMediaRecovery {
	recovery := NewBulkMediaRecovery(nil)
	recovery.RequestReupload = func(_ context.Context, _ *whatsmeow.Client, info types.MessageInfo, _ []byte, _ time.Duration) (string, error) {
		p.requests = append(p.requests, info.ID)
		return "/v/t62.7118-24/fresh.enc", p.requestErr
	}
	recovery.FetchReuploaded = func(_ context.Context, _ *whatsmeow.Client, _ string, _ whatsmeow.DownloadableMessage, storageLocation string) (utils.ReuploadedMedia, error) {
		return utils.ReuploadedMedia{ExtractedMedia: utils.ExtractedMedia{MediaPath: storageLocation + "/image.jpg"}}, nil
	}
	return recovery
}

func expiredImage(id string) *domainChatStorage.Message {
	return &domainChatStorage.Message{
		ID:        id,
		ChatJID:   "5511888888888@s.whatsapp.net",
		MediaType: "image",
		MediaKey:  []byte("0123456789abcdef0123456789abcdef"),
	}
}

func TestBulkMediaRecoveryStopsAskingAfterUnansweredRequest(t *testing.T) {
	phone := &bulkRecoveryPhone{requestErr: ErrMediaRetryTimeout}
	recovery := phone.recovery()

	_, err := recovery.Recover(context.Background(), nil, expiredImage("first"), t.TempDir(), whatsmeow.ErrMediaDownloadFailedWith404)
	require.ErrorIs(t, err, ErrMediaRetryTimeout)

	// The phone is back, but this run no longer asks it.
	phone.requestErr = nil
	_, err = recovery.Recover(context.Background(), nil, expiredImage("second"), t.TempDir(), whatsmeow.ErrMediaDownloadFailedWith410)

	require.ErrorIs(t, err, ErrMediaRetrySkipped)
	require.ErrorIs(t, err, whatsmeow.ErrMediaDownloadFailedWith410, "the original download error stays in the chain")
	require.Equal(t, []types.MessageID{"first"}, phone.requests)
}

func TestBulkMediaRecoveryKeepsAskingAfterPhoneAnswersWithoutFile(t *testing.T) {
	phone := &bulkRecoveryPhone{requestErr: whatsmeow.ErrMediaNotAvailableOnPhone}
	recovery := phone.recovery()

	_, err := recovery.Recover(context.Background(), nil, expiredImage("gone"), t.TempDir(), whatsmeow.ErrMediaDownloadFailedWith404)
	require.ErrorIs(t, err, whatsmeow.ErrMediaNotAvailableOnPhone)

	phone.requestErr = nil
	extracted, err := recovery.Recover(context.Background(), nil, expiredImage("kept"), t.TempDir(), whatsmeow.ErrMediaDownloadFailedWith404)

	require.NoError(t, err)
	require.NotEmpty(t, extracted.MediaPath)
	require.Equal(t, []types.MessageID{"gone", "kept"}, phone.requests)
}
