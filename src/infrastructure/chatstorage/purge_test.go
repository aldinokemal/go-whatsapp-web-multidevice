package chatstorage

import (
	"fmt"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
)

// The purge is retention's delete query. Its tests pin the boundary hard: what
// is strictly older than the cutoff goes, everything else stays.

var purgeCutoff = time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)

const (
	devA  = "27820000001@s.whatsapp.net"
	devB  = "27820000002@s.whatsapp.net"
	chat1 = "27831111111@s.whatsapp.net"
	chat2 = "27832222222@s.whatsapp.net"
)

func messageExists(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, repo.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE id = ? AND chat_jid = ? AND device_id = ?`, id, chatJID, deviceID).Scan(&n))
	return n == 1
}

func chatExists(t *testing.T, repo *SQLiteRepository, deviceID, chatJID string) bool {
	t.Helper()
	var n int
	require.NoError(t, repo.db.QueryRow(`SELECT COUNT(*) FROM chats WHERE jid = ? AND device_id = ?`, chatJID, deviceID).Scan(&n))
	return n == 1
}

// tableCount counts rows of a fixed table name chosen by the test.
func tableCount(t *testing.T, repo *SQLiteRepository, table string) int {
	t.Helper()
	var n int
	require.NoError(t, repo.db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&n))
	return n
}

func storeMsg(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, id string, ts time.Time) {
	t.Helper()
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
		ID: id, ChatJID: chatJID, DeviceID: deviceID, Sender: chatJID, Content: "text " + id, Timestamp: ts,
	}))
}

func storeChat(t *testing.T, repo *SQLiteRepository, deviceID, chatJID string, last time.Time) {
	t.Helper()
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{DeviceID: deviceID, JID: chatJID, Name: "name", LastMessageTime: last}))
}

// Behaviour: the boundary is "strictly before", on both devices, whatever
// time zone the timestamp was stored in.
func TestPurgeDeletesOnlyOldMessages(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	plus2 := time.FixedZone("plus2", 2*3600)
	plus14 := time.FixedZone("plus14", 14*3600)
	minus5 := time.FixedZone("minus5", -5*3600)
	for _, dev := range []string{devA, devB} {
		storeChat(t, repo, dev, chat1, purgeCutoff.Add(time.Hour))
		storeMsg(t, repo, dev, chat1, "before", purgeCutoff.Add(-time.Second))
		storeMsg(t, repo, dev, chat1, "at", purgeCutoff)
		storeMsg(t, repo, dev, chat1, "after", purgeCutoff.Add(time.Second))
		// Same instants stored in other zones: the wall clock text differs
		// from UTC, the instant does not.
		storeMsg(t, repo, dev, chat1, "before-plus14", purgeCutoff.Add(-time.Second).In(plus14))
		storeMsg(t, repo, dev, chat1, "before-plus2", purgeCutoff.Add(-time.Second).In(plus2))
		storeMsg(t, repo, dev, chat1, "after-minus5", purgeCutoff.Add(time.Second).In(minus5))
		storeMsg(t, repo, dev, chat1, "at-minus5", purgeCutoff.In(minus5))
		storeMsg(t, repo, dev, chat1, "ancient", purgeCutoff.Add(-400*24*time.Hour))
	}

	res, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, int64(8), res.Messages, "4 old messages on each of 2 devices")
	require.Zero(t, res.Chats)

	for _, dev := range []string{devA, devB} {
		for _, gone := range []string{"before", "before-plus14", "before-plus2", "ancient"} {
			require.False(t, messageExists(t, repo, dev, chat1, gone), "%s on %s should be purged", gone, dev)
		}
		for _, kept := range []string{"at", "after", "after-minus5", "at-minus5"} {
			require.True(t, messageExists(t, repo, dev, chat1, kept), "%s on %s should be kept", kept, dev)
		}
	}
}

// Behaviour: a purge with a zero cutoff deletes nothing (deletion is a
// positive act; a missing window never means "everything").
func TestPurgeWithZeroCutoffDeletesNothing(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	storeChat(t, repo, devA, chat1, purgeCutoff.Add(-48*time.Hour))
	storeMsg(t, repo, devA, chat1, "old", purgeCutoff.Add(-48*time.Hour))
	_, err := repo.PurgeMessagesBefore(time.Time{})
	require.Error(t, err)
	require.True(t, messageExists(t, repo, devA, chat1, "old"))
}

// Behaviour: an old message takes its edits, reactions, poll definition,
// Chatwoot link and queued forward with it. The same rows for a fresh
// message, and for the same message id on another device, stay.
func TestPurgeRemovesDependentRows(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	old := purgeCutoff.Add(-time.Hour)
	fresh := purgeCutoff.Add(time.Hour)
	storeChat(t, repo, devA, chat1, fresh)
	storeChat(t, repo, devB, chat1, fresh)
	storeMsg(t, repo, devA, chat1, "m-old", old)
	storeMsg(t, repo, devA, chat1, "m-fresh", fresh)
	// Same id as the old message, on another device, but fresh there.
	storeMsg(t, repo, devB, chat1, "m-old", fresh)

	addDeps := func(dev, id string, at time.Time) {
		require.NoError(t, repo.StoreReaction(&domainChatStorage.Reaction{
			MessageID: id, ChatJID: chat1, DeviceID: dev, ReactorJID: chat2, Emoji: "x", Timestamp: fresh,
		}))
		require.NoError(t, repo.StoreMessageEdit(&domainChatStorage.MessageEdit{
			OriginalMessageID: id, EditEventID: "e-" + id, ChatJID: chat1, DeviceID: dev,
			PreviousContent: "secret", NewContent: "new", EditedAt: at,
		}))
		require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
			DeviceID: dev, ChatJID: chat1, PollMessageID: id, Question: "q?", CreatedAt: at, UpdatedAt: at,
		}))
		require.NoError(t, repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
			DeviceID: dev, WhatsAppMessageID: id, WhatsAppChatJID: chat1, ChatwootMessageID: 1,
		}))
		require.NoError(t, repo.EnqueueChatwootForwardEvent(&domainChatStorage.ChatwootForwardEvent{
			DeviceID: dev, EventName: "message", WhatsAppMessageID: id, PayloadJSON: `{"text":"secret"}`, CreatedAt: at,
		}))
	}
	addDeps(devA, "m-old", old)
	addDeps(devA, "m-fresh", fresh)
	addDeps(devB, "m-old", fresh)

	res, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Messages)

	count := func(q string, args ...any) int {
		var n int
		require.NoError(t, repo.db.QueryRow(q, args...).Scan(&n))
		return n
	}
	for _, tc := range []struct {
		dev, id string
		want    int
	}{{devA, "m-old", 0}, {devA, "m-fresh", 1}, {devB, "m-old", 1}} {
		msg := fmt.Sprintf("%s on %s", tc.id, tc.dev)
		require.Equal(t, tc.want, count(`SELECT COUNT(*) FROM message_reactions WHERE device_id = ? AND message_id = ?`, tc.dev, tc.id), "reactions: "+msg)
		require.Equal(t, tc.want, count(`SELECT COUNT(*) FROM message_edits WHERE device_id = ? AND original_message_id = ?`, tc.dev, tc.id), "edits: "+msg)
		require.Equal(t, tc.want, count(`SELECT COUNT(*) FROM poll_definitions WHERE device_id = ? AND poll_message_id = ?`, tc.dev, tc.id), "polls: "+msg)
		require.Equal(t, tc.want, count(`SELECT COUNT(*) FROM chatwoot_message_links WHERE device_id = ? AND wa_message_id = ?`, tc.dev, tc.id), "links: "+msg)
		require.Equal(t, tc.want, count(`SELECT COUNT(*) FROM chatwoot_forward_queue WHERE device_id = ? AND wa_message_id = ?`, tc.dev, tc.id), "queue: "+msg)
	}
}

// Behaviour: content whose message was never stored (an edit or reaction to
// a message we do not have) still goes once it is itself past the window; a
// fresh one stays.
func TestPurgeRemovesOldOrphanedContent(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	old := purgeCutoff.Add(-time.Hour)
	fresh := purgeCutoff.Add(time.Hour)
	for id, at := range map[string]time.Time{"orphan-old": old, "orphan-fresh": fresh} {
		require.NoError(t, repo.StoreReaction(&domainChatStorage.Reaction{
			MessageID: id, ChatJID: chat1, DeviceID: devA, ReactorJID: chat2, Emoji: "x", Timestamp: at,
		}))
		require.NoError(t, repo.StoreMessageEdit(&domainChatStorage.MessageEdit{
			OriginalMessageID: id, EditEventID: "e", ChatJID: chat1, DeviceID: devA, PreviousContent: "secret", EditedAt: at,
		}))
		require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
			DeviceID: devA, ChatJID: chat1, PollMessageID: id, Question: "q?", CreatedAt: at, UpdatedAt: at,
		}))
		require.NoError(t, repo.EnqueueChatwootForwardEvent(&domainChatStorage.ChatwootForwardEvent{
			DeviceID: devA, EventName: "message", WhatsAppMessageID: id, PayloadJSON: `{"text":"secret"}`, CreatedAt: at,
		}))
	}
	_, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	for _, table := range []string{"message_reactions", "message_edits", "poll_definitions", "chatwoot_forward_queue"} {
		require.Equal(t, 1, tableCount(t, repo, table), "only the fresh orphan stays in %s", table)
	}
}

// Behaviour: a chat with one fresh message and many old ones keeps the chat
// and the fresh message.
func TestPurgeKeepsAChatWithAFreshMessage(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	storeChat(t, repo, devA, chat1, purgeCutoff.Add(time.Minute))
	for i := 0; i < 20; i++ {
		storeMsg(t, repo, devA, chat1, fmt.Sprintf("old-%d", i), purgeCutoff.Add(-time.Duration(i+1)*time.Hour))
	}
	storeMsg(t, repo, devA, chat1, "fresh", purgeCutoff.Add(time.Minute))

	res, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, int64(20), res.Messages)
	require.Zero(t, res.Chats)
	require.True(t, chatExists(t, repo, devA, chat1))
	require.True(t, messageExists(t, repo, devA, chat1, "fresh"))
}

// Behaviour: a chat left with no messages goes only when its own last
// message time is also past the window, and only on its own device.
func TestPurgeRemovesAnEmptyOldChat(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	storeChat(t, repo, devA, chat1, purgeCutoff.Add(-time.Hour))
	storeMsg(t, repo, devA, chat1, "old", purgeCutoff.Add(-time.Hour))
	// Same chat on another device with a fresh message.
	storeChat(t, repo, devB, chat1, purgeCutoff.Add(time.Hour))
	storeMsg(t, repo, devB, chat1, "fresh", purgeCutoff.Add(time.Hour))
	// An empty chat whose last message time is fresh (a message we did not
	// store, or a just-created chat): kept.
	storeChat(t, repo, devA, chat2, purgeCutoff.Add(time.Hour))
	// An empty chat at exactly the cutoff: kept (strictly before).
	storeChat(t, repo, devB, chat2, purgeCutoff)

	res, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Chats)
	require.False(t, chatExists(t, repo, devA, chat1))
	require.True(t, chatExists(t, repo, devB, chat1))
	require.True(t, chatExists(t, repo, devA, chat2))
	require.True(t, chatExists(t, repo, devB, chat2))
}

// Behaviour: pending (and every other) scheduled send, the device registry and
// Chatwoot device configs are configuration and are never touched.
func TestPurgeNeverTouchesScheduledSends(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	old := purgeCutoff.Add(-72 * time.Hour)
	for i, status := range []string{"active", "paused", "completed"} {
		require.NoError(t, repo.CreateScheduledSend(&domainChatStorage.ScheduledSend{
			ID: fmt.Sprintf("s-%d", i), DeviceID: devA, MessageType: "text", PayloadJSON: `{"message":"hi"}`,
			Phone: "27", Summary: "hi", ScheduledAt: old, NextRunAt: old, Timezone: "UTC", Recurrence: "once",
			WeekdaysJSON: "[]", Status: status,
		}))
	}
	require.NoError(t, repo.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: devA, JID: devA, CreatedAt: old, UpdatedAt: old}))
	storeChat(t, repo, devA, chat1, old)
	storeMsg(t, repo, devA, chat1, "old", old)

	_, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, 3, tableCount(t, repo, "scheduled_sends"))
	require.Equal(t, 1, tableCount(t, repo, "devices"))
	require.Zero(t, tableCount(t, repo, "messages"))
}

// Mechanism: a large backlog goes in several committed batches, and all of it
// goes.
func TestPurgeBatchesLargeBacklogs(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	tx, err := repo.db.Begin()
	require.NoError(t, err)
	stmt, err := tx.Prepare(`INSERT INTO messages (id, chat_jid, device_id, sender, content, timestamp) VALUES (?, ?, ?, ?, ?, ?)`)
	require.NoError(t, err)
	for i := 0; i < 12001; i++ {
		_, err := stmt.Exec(fmt.Sprintf("m-%d", i), chat1, devA, chat1, "x", purgeCutoff.Add(-time.Duration(i+1)*time.Second))
		require.NoError(t, err)
	}
	_, err = stmt.Exec("keep", chat1, devA, chat1, "x", purgeCutoff.Add(time.Second))
	require.NoError(t, err)
	require.NoError(t, stmt.Close())
	require.NoError(t, tx.Commit())

	res, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, int64(12001), res.Messages)
	require.Equal(t, 1, tableCount(t, repo, "messages"))

	// With a small batch, the purge commits more than once.
	for i := 0; i < 250; i++ {
		storeMsg(t, repo, devA, chat1, fmt.Sprintf("again-%d", i), purgeCutoff.Add(-time.Minute))
	}
	commits := 0
	restore := setPurgeBatchForTest(100, func() { commits++ })
	defer restore()
	res, err = repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, int64(250), res.Messages)
	require.GreaterOrEqual(t, commits, 3)
	require.Equal(t, 1, tableCount(t, repo, "messages"))
}

// Behaviour: every form a driver writes reads back as the same instant, and
// text in no known form is never judged old (so never deleted).
func TestStoredTimesParseToTheInstant(t *testing.T) {
	want := time.Date(2026, time.November, 23, 9, 59, 59, 500000000, time.UTC)
	for _, s := range []string{
		"2026-11-23 11:59:59.5+02:00",
		"2026-11-23 09:59:59.5+00:00",
		"2026-11-23 11:59:59.5 +0200 SAST",
		"2026-11-23 11:59:59.5 +0200 +02",
		"2026-11-23 11:59:59.5 +0200 SAST m=+12.345",
		"2026-11-23T09:59:59.5Z",
		"2026-11-23 09:59:59.5",
	} {
		got, ok := parseStoredTime(s)
		require.True(t, ok, s)
		require.True(t, want.Equal(got), "%s read as %s", s, got)
	}
	for _, s := range []string{"", "yesterday", "1732352400", "2026-13-45 99:00:00"} {
		_, ok := parseStoredTime(s)
		require.False(t, ok, s)
		require.False(t, olderThan(s, purgeCutoff), s)
	}
}

// Behaviour: a message whose timestamp cannot be read is kept.
func TestPurgeKeepsUnreadableTimestamps(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	_, err := repo.db.Exec(`INSERT INTO messages (id, chat_jid, device_id, sender, content, timestamp) VALUES ('odd', ?, ?, ?, 'x', 'not a time')`, chat1, devA, chat1)
	require.NoError(t, err)
	_, err = repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.True(t, messageExists(t, repo, devA, chat1, "odd"))
}

// Behaviour: Chatwoot rows are keyed by device and message id only. When the
// same id is stored in two chats on one device and only one copy is old, the
// Chatwoot rows stay with the copy that is left.
func TestPurgeKeepsChatwootRowsWhileAMessageWithTheIDRemains(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	storeChat(t, repo, devA, chat1, purgeCutoff.Add(time.Hour))
	storeChat(t, repo, devA, chat2, purgeCutoff.Add(time.Hour))
	storeMsg(t, repo, devA, chat1, "same", purgeCutoff.Add(-time.Hour))
	storeMsg(t, repo, devA, chat2, "same", purgeCutoff.Add(time.Hour))
	require.NoError(t, repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID: devA, WhatsAppMessageID: "same", WhatsAppChatJID: chat2, ChatwootMessageID: 1,
	}))
	require.NoError(t, repo.EnqueueChatwootForwardEvent(&domainChatStorage.ChatwootForwardEvent{
		DeviceID: devA, EventName: "message", WhatsAppMessageID: "same", PayloadJSON: `{}`, CreatedAt: purgeCutoff.Add(time.Hour),
	}))
	res, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Messages)
	require.Equal(t, 1, tableCount(t, repo, "chatwoot_message_links"))
	require.Equal(t, 1, tableCount(t, repo, "chatwoot_forward_queue"))
}

// Behaviour: a stored fresh message keeps its rows even when their own time
// reads old (a skewed clock on the sender's phone, say).
func TestPurgeKeepsOldLookingRowsOfAFreshMessage(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	old := purgeCutoff.Add(-time.Hour)
	storeChat(t, repo, devA, chat1, purgeCutoff.Add(time.Hour))
	storeMsg(t, repo, devA, chat1, "fresh", purgeCutoff.Add(time.Hour))
	require.NoError(t, repo.StoreReaction(&domainChatStorage.Reaction{
		MessageID: "fresh", ChatJID: chat1, DeviceID: devA, ReactorJID: chat2, Emoji: "x", Timestamp: old,
	}))
	require.NoError(t, repo.StoreMessageEdit(&domainChatStorage.MessageEdit{
		OriginalMessageID: "fresh", EditEventID: "e", ChatJID: chat1, DeviceID: devA, EditedAt: old,
	}))
	require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: devA, ChatJID: chat1, PollMessageID: "fresh", Question: "q?", CreatedAt: old, UpdatedAt: old,
	}))
	require.NoError(t, repo.EnqueueChatwootForwardEvent(&domainChatStorage.ChatwootForwardEvent{
		DeviceID: devA, EventName: "message", WhatsAppMessageID: "fresh", PayloadJSON: `{}`, CreatedAt: old,
	}))
	_, err := repo.PurgeMessagesBefore(purgeCutoff)
	require.NoError(t, err)
	for _, table := range []string{"message_reactions", "message_edits", "poll_definitions", "chatwoot_forward_queue"} {
		require.Equal(t, 1, tableCount(t, repo, table), table)
	}
}
