package chatstorage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// wrapSendMessage persists a sent message from a goroutine, so an edit can reach
// storage before the original write does. These pin the resulting order, without
// depending on the scheduler: the writes are issued in the order the race
// produces, which is what a blocked original store amounts to.

const (
	orderChatJID = "628123456789@s.whatsapp.net"
	orderDevice  = "device-1@s.whatsapp.net"
	orderMsgID   = "MSG-ORDER-1"
)

func orderTestRepo(t *testing.T) (*SQLiteRepository, context.Context) {
	t.Helper()
	repo := NewStorageRepository(openTestDB(t)).(*SQLiteRepository)
	require.NoError(t, repo.InitializeSchema())
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(orderDevice, nil, nil))
	return repo, ctx
}

func editEvent(id, newContent string, at time.Time) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("628123456789", types.DefaultUserServer),
				Sender:   types.NewJID("628123456789", types.DefaultUserServer),
				IsFromMe: true,
			},
			ID:        "EDIT-EVT-1",
			Timestamp: at,
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key: &waCommon.MessageKey{
				ID:        proto.String(id),
				RemoteJID: proto.String(orderChatJID),
				FromMe:    proto.Bool(true),
			},
			EditedMessage: &waE2E.Message{Conversation: proto.String(newContent)},
		}},
	}
}

// The race: the edit lands first, then the delayed original store arrives. The
// original content must NOT come back — that is the stale quote all over again.
func TestDelayedSentStoreDoesNotOverwriteAnEarlierEdit(t *testing.T) {
	repo, ctx := orderTestRepo(t)
	now := time.Now().UTC()

	// 1. the edit reaches storage first (original row does not exist yet)
	require.NoError(t, repo.CreateMessage(ctx, editEvent(orderMsgID, "EDITED content", now)))

	// 2. the goroutine from wrapSendMessage finally runs, carrying the ORIGINAL
	require.NoError(t, repo.StoreSentMessageWithContext(
		ctx, orderMsgID, "628123456789@s.whatsapp.net", orderChatJID,
		"ORIGINAL content", now, nil,
	))

	stored, err := repo.GetMessageByIDAndDevice(orderDevice, orderMsgID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "EDITED content", stored.Content,
		"the delayed original store rolled the edit back")
}

// The ordinary order still behaves: store, then edit, edit wins.
func TestEditAfterTheSentStoreStillApplies(t *testing.T) {
	repo, ctx := orderTestRepo(t)
	now := time.Now().UTC()

	require.NoError(t, repo.StoreSentMessageWithContext(
		ctx, orderMsgID, "628123456789@s.whatsapp.net", orderChatJID,
		"ORIGINAL content", now, nil,
	))
	require.NoError(t, repo.CreateMessage(ctx, editEvent(orderMsgID, "EDITED content", now.Add(time.Second))))

	stored, err := repo.GetMessageByIDAndDevice(orderDevice, orderMsgID)
	require.NoError(t, err)
	assert.Equal(t, "EDITED content", stored.Content)
}

// Control: with no edit recorded, a sent store writes its content normally. The
// guard must not freeze content for every message that happens to be re-stored.
func TestSentStoreWritesContentWhenNoEditExists(t *testing.T) {
	repo, ctx := orderTestRepo(t)
	now := time.Now().UTC()

	require.NoError(t, repo.StoreSentMessageWithContext(
		ctx, orderMsgID, "628123456789@s.whatsapp.net", orderChatJID, "first", now, nil))
	require.NoError(t, repo.StoreSentMessageWithContext(
		ctx, orderMsgID, "628123456789@s.whatsapp.net", orderChatJID, "second", now, nil))

	stored, err := repo.GetMessageByIDAndDevice(orderDevice, orderMsgID)
	require.NoError(t, err)
	assert.Equal(t, "second", stored.Content)
}

// Interleaves the two writers repeatedly. The real guarantee is structural — the
// edit check is an EXISTS inside the UPDATE, so there is no window between check
// and write — and this exercises it under actual contention rather than relying
// on a hand-picked order. Content must never come back as the original.
func TestConcurrentSentStoreAndEditNeverYieldsOriginalContent(t *testing.T) {
	const rounds = 40

	for i := 0; i < rounds; i++ {
		repo, ctx := orderTestRepo(t)
		now := time.Now().UTC()
		id := orderMsgID

		var wg sync.WaitGroup
		wg.Add(2)
		start := make(chan struct{})

		go func() {
			defer wg.Done()
			<-start
			_ = repo.StoreSentMessageWithContext(
				ctx, id, "628123456789@s.whatsapp.net", orderChatJID, "ORIGINAL content", now, nil)
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = repo.CreateMessage(ctx, editEvent(id, "EDITED content", now))
		}()

		close(start)
		wg.Wait()

		stored, err := repo.GetMessageByIDAndDevice(orderDevice, id)
		require.NoError(t, err)
		if stored == nil {
			continue // both writers failed to land a row; nothing to assert
		}
		// The edit may or may not have won the ordering, but once it has been
		// RECORDED the stored content must reflect it — never the original.
		edits, err := repo.GetMessageEdits(id, orderDevice)
		require.NoError(t, err)
		if len(edits) > 0 {
			require.Equal(t, "EDITED content", stored.Content,
				"round %d: the original store clobbered a recorded edit", i)
		}
	}
}
