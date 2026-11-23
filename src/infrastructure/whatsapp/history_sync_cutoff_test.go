package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var testHistoryCutoff = time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)

// historyCutoffSpy records everything history sync tries to store.
type historyCutoffSpy struct {
	domainChatStorage.IChatStorageRepository
	chats     map[string]*domainChatStorage.Chat
	messages  []*domainChatStorage.Message
	reactions []string
	polls     []string
}

func newHistoryCutoffSpy() *historyCutoffSpy {
	return &historyCutoffSpy{chats: map[string]*domainChatStorage.Chat{}}
}

func (r *historyCutoffSpy) StoreChat(chat *domainChatStorage.Chat) error {
	r.chats[chat.JID] = chat
	return nil
}
func (r *historyCutoffSpy) GetChatByDevice(_, jid string) (*domainChatStorage.Chat, error) {
	return r.chats[jid], nil
}
func (r *historyCutoffSpy) StoreMessagesBatch(messages []*domainChatStorage.Message) error {
	r.messages = append(r.messages, messages...)
	return nil
}
func (r *historyCutoffSpy) CreateReaction(_ context.Context, evt *events.Message) error {
	r.reactions = append(r.reactions, evt.Info.ID)
	return nil
}
func (r *historyCutoffSpy) UpsertPollDefinition(d *domainChatStorage.PollDefinition) error {
	r.polls = append(r.polls, d.PollMessageID)
	return nil
}
func (r *historyCutoffSpy) GetChatNameWithPushName(jid types.JID, _ string, _ string, pushName string) string {
	if pushName != "" {
		return pushName
	}
	return jid.String()
}

func (r *historyCutoffSpy) messageIDs() []string {
	var ids []string
	for _, m := range r.messages {
		ids = append(ids, m.ID)
	}
	return ids
}

func histText(chatJID, id string, at time.Time) *waHistorySync.HistorySyncMsg {
	ts := uint64(at.Unix())
	return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key:              &waCommon.MessageKey{RemoteJID: proto.String(chatJID), FromMe: proto.Bool(false), ID: proto.String(id)},
		Message:          &waE2E.Message{Conversation: proto.String("text " + id)},
		MessageTimestamp: &ts,
	}}
}

func histReaction(chatJID, id, target string, at time.Time) *waHistorySync.HistorySyncMsg {
	ts := uint64(at.Unix())
	return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key: &waCommon.MessageKey{RemoteJID: proto.String(chatJID), FromMe: proto.Bool(false), ID: proto.String(id)},
		Message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
			Key:  &waCommon.MessageKey{RemoteJID: proto.String(chatJID), ID: proto.String(target)},
			Text: proto.String("x"),
		}},
		MessageTimestamp: &ts,
	}}
}

func histPoll(chatJID, id string, at time.Time) *waHistorySync.HistorySyncMsg {
	ts := uint64(at.Unix())
	return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
		Key: &waCommon.MessageKey{RemoteJID: proto.String(chatJID), FromMe: proto.Bool(false), ID: proto.String(id)},
		Message: &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{
			Name:    proto.String("lunch?"),
			Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("yes")}},
		}},
		MessageTimestamp: &ts,
	}}
}

const (
	mixedChat = "27831111111@s.whatsapp.net"
	oldChat   = "27832222222@s.whatsapp.net"
)

func cutoffHistory() *waHistorySync.HistorySync {
	syncType := waHistorySync.HistorySync_INITIAL_BOOTSTRAP
	return &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{
			{ID: proto.String(mixedChat), Messages: []*waHistorySync.HistorySyncMsg{
				histText(mixedChat, "old-1", testHistoryCutoff.Add(-30*24*time.Hour)),
				histText(mixedChat, "old-2", testHistoryCutoff.Add(-time.Second)),
				histText(mixedChat, "at", testHistoryCutoff),
				histText(mixedChat, "fresh", testHistoryCutoff.Add(time.Hour)),
				histReaction(mixedChat, "react-old", "old-1", testHistoryCutoff.Add(-time.Hour)),
				histReaction(mixedChat, "react-fresh", "fresh", testHistoryCutoff.Add(time.Hour)),
				histPoll(mixedChat, "poll-old", testHistoryCutoff.Add(-time.Hour)),
				histPoll(mixedChat, "poll-fresh", testHistoryCutoff.Add(time.Hour)),
			}},
			{ID: proto.String(oldChat), Messages: []*waHistorySync.HistorySyncMsg{
				histText(oldChat, "ancient", testHistoryCutoff.Add(-365*24*time.Hour)),
			}},
		},
	}
}

func withCutoffHook(t *testing.T, hook func() (time.Time, bool)) {
	t.Helper()
	saved, savedLog := HistoryCutoffHook, log
	HistoryCutoffHook, log = hook, waLog.Noop
	t.Cleanup(func() { HistoryCutoffHook, log = saved, savedLog })
}

// Behaviour: with the hook set, nothing older than the cutoff is stored
// (messages, reactions or poll definitions), and a chat whose every message
// was dropped is not created. The boundary is "strictly before".
func TestHistorySyncSkipsOldMessagesWhenHookSet(t *testing.T) {
	withCutoffHook(t, func() (time.Time, bool) { return testHistoryCutoff, true })
	repo := newHistoryCutoffSpy()
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance("27820000001@s.whatsapp.net", nil, nil))

	require.NoError(t, processHistorySync(ctx, cutoffHistory(), repo, nil))

	require.ElementsMatch(t, []string{"at", "fresh"}, repo.messageIDs())
	require.Equal(t, []string{"react-fresh"}, repo.reactions)
	require.Equal(t, []string{"poll-fresh"}, repo.polls)
	require.Contains(t, repo.chats, mixedChat)
	require.True(t, repo.chats[mixedChat].LastMessageTime.Equal(testHistoryCutoff.Add(time.Hour)))
	require.NotContains(t, repo.chats, oldChat, "a chat with only old messages is not created")
}

// Behaviour: without the hook (upstream, single-user) everything is stored.
func TestHistorySyncUnchangedWithoutHook(t *testing.T) {
	for name, hook := range map[string]func() (time.Time, bool){
		"nil":    nil,
		"not ok": func() (time.Time, bool) { return testHistoryCutoff, false },
		"zero":   func() (time.Time, bool) { return time.Time{}, true },
	} {
		t.Run(name, func(t *testing.T) {
			withCutoffHook(t, hook)
			repo := newHistoryCutoffSpy()
			ctx := ContextWithDevice(context.Background(), NewDeviceInstance("27820000001@s.whatsapp.net", nil, nil))
			require.NoError(t, processHistorySync(ctx, cutoffHistory(), repo, nil))
			require.Len(t, repo.messages, 5)
			require.Len(t, repo.reactions, 2)
			require.Len(t, repo.polls, 2)
			require.Contains(t, repo.chats, oldChat)
		})
	}
}

// Behaviour: push names are contact data and are still stored with the hook on.
func TestHistorySyncStillStoresPushNames(t *testing.T) {
	withCutoffHook(t, func() (time.Time, bool) { return testHistoryCutoff, true })
	repo := newHistoryCutoffSpy()
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance("27820000001@s.whatsapp.net", nil, nil))
	syncType := waHistorySync.HistorySync_PUSH_NAME
	data := &waHistorySync.HistorySync{
		SyncType:  &syncType,
		Pushnames: []*waHistorySync.Pushname{{ID: proto.String(oldChat), Pushname: proto.String("Thandi")}},
	}
	require.NoError(t, processHistorySync(ctx, data, repo, nil))
	require.Contains(t, repo.chats, oldChat)
	require.Equal(t, "Thandi", repo.chats[oldChat].Name)
}

// Behaviour: the raw history dump (which holds every message, however old)
// is not written to disk while retention is on.
func TestHistoryDumpSkippedWhileRetentionIsOn(t *testing.T) {
	withCutoffHook(t, nil)
	require.True(t, historyDumpAllowed())
	withCutoffHook(t, func() (time.Time, bool) { return testHistoryCutoff, false })
	require.True(t, historyDumpAllowed())
	withCutoffHook(t, func() (time.Time, bool) { return testHistoryCutoff, true })
	require.False(t, historyDumpAllowed())
}
