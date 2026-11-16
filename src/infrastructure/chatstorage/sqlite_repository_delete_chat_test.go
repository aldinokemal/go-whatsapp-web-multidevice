package chatstorage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

var deletedChatTables = []struct{ name, scopedCount, totalCount, aliasesCount string }{
	{"messages", "SELECT COUNT(*) FROM messages WHERE device_id = ? AND chat_jid = ?", "SELECT COUNT(*) FROM messages", "SELECT COUNT(*) FROM messages WHERE device_id = ? AND chat_jid IN (?,?)"},
	{"message_reactions", "SELECT COUNT(*) FROM message_reactions WHERE device_id = ? AND chat_jid = ?", "SELECT COUNT(*) FROM message_reactions", "SELECT COUNT(*) FROM message_reactions WHERE device_id = ? AND chat_jid IN (?,?)"},
	{"message_edits", "SELECT COUNT(*) FROM message_edits WHERE device_id = ? AND chat_jid = ?", "SELECT COUNT(*) FROM message_edits", "SELECT COUNT(*) FROM message_edits WHERE device_id = ? AND chat_jid IN (?,?)"},
	{"poll_definitions", "SELECT COUNT(*) FROM poll_definitions WHERE device_id = ? AND chat_jid = ?", "SELECT COUNT(*) FROM poll_definitions", "SELECT COUNT(*) FROM poll_definitions WHERE device_id = ? AND chat_jid IN (?,?)"},
	{"chatwoot_message_links", "SELECT COUNT(*) FROM chatwoot_message_links WHERE device_id = ? AND wa_chat_jid = ?", "SELECT COUNT(*) FROM chatwoot_message_links", "SELECT COUNT(*) FROM chatwoot_message_links WHERE device_id = ? AND wa_chat_jid IN (?,?)"},
	{"chats", "SELECT COUNT(*) FROM chats WHERE device_id = ? AND jid = ?", "SELECT COUNT(*) FROM chats", "SELECT COUNT(*) FROM chats WHERE device_id = ? AND jid IN (?,?)"},
}

func seedDeleteChat(t *testing.T, repo *SQLiteRepository, device, jid string, messageID ...string) {
	t.Helper()
	id := "msg-" + jid
	if len(messageID) > 0 {
		id = messageID[0]
	}
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{DeviceID: device, JID: jid}))
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{DeviceID: device, ChatJID: jid, ID: id, Content: "hello", Timestamp: time.Now()}))
	for _, query := range []string{
		`INSERT INTO message_reactions (device_id,chat_jid,message_id,reactor_jid,reaction_timestamp) VALUES (?,?,?,'sender',CURRENT_TIMESTAMP)`,
		`INSERT INTO message_edits (device_id,chat_jid,original_message_id,edit_event_id,edited_at) VALUES (?,?,?,'edit',CURRENT_TIMESTAMP)`,
		`INSERT INTO poll_definitions (device_id,chat_jid,poll_message_id) VALUES (?,?,?)`,
		`INSERT INTO chatwoot_message_links (device_id,wa_chat_jid,wa_message_id) VALUES (?,?,?)`,
	} {
		_, err := repo.db.Exec(query, device, jid, id)
		require.NoError(t, err)
	}
}

func TestDeleteChatByDevicePurgesAliasesAndPreservesOthers(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	pn, lid, other := "628111@s.whatsapp.net", "999111@lid", "628222@s.whatsapp.net"
	for _, device := range []string{"device-a", "device-b"} {
		for _, jid := range []string{pn, lid, other} {
			seedDeleteChat(t, repo, device, jid)
		}
	}
	require.NoError(t, repo.DeleteChatByDevice("device-a", pn, lid))
	require.NoError(t, repo.DeleteChatByDevice("device-a", pn, lid), "idempotent")
	for _, table := range deletedChatTables {
		for _, device := range []string{"device-a", "device-b"} {
			for _, jid := range []string{pn, lid, other} {
				want := 1
				if device == "device-a" && jid != other {
					want = 0
				}
				var got int
				require.NoError(t, repo.db.QueryRow(table.scopedCount, device, jid).Scan(&got))
				require.Equal(t, want, got, "%s %s %s", table.name, device, jid)
			}
		}
	}
	require.Error(t, repo.DeleteChatByDevice("", other))
}

func TestDeleteChatByDeviceRollsBackAllAliases(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	for _, jid := range []string{"chat-a", "chat-b"} {
		seedDeleteChat(t, repo, "device-a", jid)
	}
	_, err := repo.db.Exec(`CREATE TRIGGER fail_delete_chat BEFORE DELETE ON chats WHEN OLD.jid = 'chat-b' BEGIN SELECT RAISE(ABORT, 'purge failed'); END`)
	require.NoError(t, err)
	require.Error(t, repo.DeleteChatByDevice("device-a", "chat-a", "chat-b"))
	for _, table := range deletedChatTables {
		var got int
		require.NoError(t, repo.db.QueryRow(table.totalCount).Scan(&got))
		require.Equal(t, 2, got, table.name)
	}
}

func TestIsChatMediaDirectoryReferencedIncludesAllDevicesAndOrphanMessages(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{ID: "orphan", DeviceID: "other", ChatJID: "120-456@g.us", Content: "hello"}))
	used, err := repo.IsChatMediaDirectoryReferenced("120")
	require.NoError(t, err)
	require.True(t, used)
	used, err = repo.IsChatMediaDirectoryReferenced("456")
	require.NoError(t, err)
	require.False(t, used)
}

func TestDeleteChatByDeviceBeforeKeepsNewerMessagesAndDependencies(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	bound := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	pn, lid, other := "628111@s.whatsapp.net", "999111@lid", "628222@s.whatsapp.net"
	for _, device := range []string{"device-a", "device-b"} {
		for _, jid := range []string{pn, lid, other} {
			for _, id := range []string{"old-" + jid, "new-" + jid} {
				seedDeleteChat(t, repo, device, jid, id)
				ts := bound
				if id == "new-"+jid {
					ts = bound.Add(time.Minute)
				}
				_, err := repo.db.Exec("UPDATE messages SET timestamp = ? WHERE device_id = ? AND chat_jid = ? AND id = ?", ts, device, jid, id)
				require.NoError(t, err)
			}
		}
	}
	removed, err := repo.DeleteChatByDeviceBefore("device-a", bound, pn, lid)
	require.NoError(t, err)
	require.False(t, removed)
	for _, table := range deletedChatTables {
		for _, device := range []string{"device-a", "device-b"} {
			for _, jid := range []string{pn, lid, other} {
				var count int
				require.NoError(t, repo.db.QueryRow(table.scopedCount, device, jid).Scan(&count))
				want := 2
				if table.name == "chats" || (device == "device-a" && jid != other) {
					want = 1
				}
				require.Equal(t, want, count, "%s %s %s", table.name, device, jid)
			}
		}
	}
	// A replay has the same result. It cannot remove the new rows or either chat row.
	removed, err = repo.DeleteChatByDeviceBefore("device-a", bound, pn, lid)
	require.NoError(t, err)
	require.False(t, removed)
	// A range that covers every message removes both chat rows.
	removed, err = repo.DeleteChatByDeviceBefore("device-a", bound.Add(time.Minute), pn, lid)
	require.NoError(t, err)
	require.True(t, removed)
	for _, table := range deletedChatTables {
		var count int
		require.NoError(t, repo.db.QueryRow(table.aliasesCount, "device-a", pn, lid).Scan(&count))
		require.Zero(t, count, table.name)
	}
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{DeviceID: "device-a", JID: pn}))
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{DeviceID: "device-a", ChatJID: lid, ID: "newer", Content: "hello", Timestamp: bound.Add(time.Hour)}))
	removed, err = repo.DeleteChatByDeviceBefore("device-a", bound, pn, lid)
	require.NoError(t, err)
	require.False(t, removed)
	kept, err := repo.GetChatByDevice("device-a", pn)
	require.NoError(t, err)
	require.NotNil(t, kept, "a newer message in either alias must keep both chat rows")
}

func TestDeleteChatByDeviceBeforeRollsBackAllAliases(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	bound := time.Now().Add(time.Hour)
	for _, jid := range []string{"chat-a", "chat-b"} {
		seedDeleteChat(t, repo, "device-a", jid)
	}
	_, err := repo.db.Exec(`CREATE TRIGGER fail_bounded_delete BEFORE DELETE ON chats WHEN OLD.jid = 'chat-b' BEGIN SELECT RAISE(ABORT, 'purge failed'); END`)
	require.NoError(t, err)
	removed, err := repo.DeleteChatByDeviceBefore("device-a", bound, "chat-a", "chat-b")
	require.Error(t, err)
	require.False(t, removed)
	for _, table := range deletedChatTables {
		var count int
		require.NoError(t, repo.db.QueryRow(table.totalCount).Scan(&count))
		require.Equal(t, 2, count, table.name)
	}
}

func TestDeleteChatByDeviceBeforeUsesInstantAcrossZones(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	base := time.Date(2026, 11, 12, 22, 4, 12, 0, time.UTC)
	for i, offset := range []time.Duration{-5 * time.Hour, -time.Second, 0, time.Second, 5 * time.Hour} {
		require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
			DeviceID: "device-a", ChatJID: "chat-a", ID: fmt.Sprintf("m%d", i), Content: "hello", Timestamp: base.Add(offset).In(time.FixedZone([]string{"PET", "UTC", "JST"}[i%3], []int{-5 * 3600, 0, 9 * 3600}[i%3])),
		}))
	}
	removed, err := repo.DeleteChatByDeviceBefore("device-a", base.In(time.FixedZone("PET", -5*3600)), "chat-a")
	require.NoError(t, err)
	require.False(t, removed)
	rows, err := repo.db.Query("SELECT id FROM messages WHERE device_id = ? AND chat_jid = ? ORDER BY id", "device-a", "chat-a")
	require.NoError(t, err)
	defer rows.Close()
	var kept []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		kept = append(kept, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"m3", "m4"}, kept)
}

func TestBoundedDeletePollOrphansAndSameSecond(t *testing.T) {
	for _, survivors := range []bool{false, true} {
		t.Run(fmt.Sprint(survivors), func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			bound := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			seedDeleteChat(t, repo, "device", "chat", "same-second")
			require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{ID: "same-second", DeviceID: "device", ChatJID: "chat", Content: "hello", Timestamp: bound.Add(900 * time.Millisecond)}))
			if survivors {
				require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{ID: "new", DeviceID: "device", ChatJID: "chat", Content: "hello", Timestamp: bound.Add(time.Second)}))
			}
			for _, tc := range []struct {
				id string
				ts time.Time
			}{{"before", bound.Add(-time.Second)}, {"at", bound}, {"after", bound.Add(time.Second)}} {
				_, err := repo.db.Exec("INSERT INTO poll_definitions(device_id,chat_jid,poll_message_id,created_at) VALUES (?,?,?,?)", "device", "chat", tc.id, tc.ts.In(time.FixedZone("JST", 9*3600)))
				require.NoError(t, err)
			}
			// Orphan dependencies must also be cleared when no messages survive.
			seedDeleteChat(t, repo, "device", "chat", "orphan")
			_, err := repo.db.Exec("DELETE FROM messages WHERE id = ?", "orphan")
			require.NoError(t, err)
			_, err = repo.db.Exec("UPDATE poll_definitions SET created_at = ? WHERE poll_message_id = ?", bound, "orphan")
			require.NoError(t, err)
			removed, err := repo.DeleteChatByDeviceBefore("device", bound, "chat")
			require.NoError(t, err)
			require.Equal(t, !survivors, removed)
			var count int
			require.NoError(t, repo.db.QueryRow("SELECT COUNT(*) FROM messages WHERE id = ?", "same-second").Scan(&count))
			require.Zero(t, count, "the bound includes all messages in its second")
			require.NoError(t, repo.db.QueryRow("SELECT COUNT(*) FROM poll_definitions").Scan(&count))
			require.Equal(t, 1, count)
			var id string
			require.NoError(t, repo.db.QueryRow("SELECT poll_message_id FROM poll_definitions").Scan(&id))
			require.Equal(t, "after", id)
			if !survivors {
				for _, q := range []string{"SELECT COUNT(*) FROM message_reactions", "SELECT COUNT(*) FROM message_edits", "SELECT COUNT(*) FROM chatwoot_message_links"} {
					require.NoError(t, repo.db.QueryRow(q).Scan(&count))
					require.Zero(t, count)
				}
			}
		})
	}
}

func TestBoundedDeleteInvalidTimestampRollsBack(t *testing.T) {
	for _, query := range []string{
		"UPDATE messages SET timestamp = 'not-a-time'",
		"DELETE FROM messages; UPDATE poll_definitions SET created_at = 'not-a-time'",
	} {
		t.Run(query, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			seedDeleteChat(t, repo, "device", "chat")
			_, err := repo.db.Exec(query)
			require.NoError(t, err)
			removed, err := repo.DeleteChatByDeviceBefore("device", time.Now(), "chat")
			require.Error(t, err)
			require.False(t, removed)
			var count int
			require.NoError(t, repo.db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&count))
			require.Equal(t, 1, count)
			require.NoError(t, repo.db.QueryRow("SELECT COUNT(*) FROM poll_definitions").Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestChatMediaReferenceQueryUsesIndexes(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	rows, err := repo.db.Query("EXPLAIN QUERY PLAN "+chatMediaReferencesQuery, "120", "120\xff", "120", "120\xff")
	require.NoError(t, err)
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &detail))
		plans = append(plans, detail)
		if strings.Contains(detail, "chats") || strings.Contains(detail, "messages") {
			require.Contains(t, detail, "SEARCH")
			require.Contains(t, detail, "INDEX")
		}
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, plans)
}

func TestPurgeLocalChatKeepsLegacyGroupMedia(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	old := config.PathMedia
	config.PathMedia = t.TempDir()
	t.Cleanup(func() { config.PathMedia = old })
	dir := filepath.Join(config.PathMedia, "120")
	require.NoError(t, os.Mkdir(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "group.jpg"), []byte("keep"), 0600))
	seedDeleteChat(t, repo, "device", "120@s.whatsapp.net")
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{DeviceID: "other", JID: "120-456@g.us"}))
	require.NoError(t, whatsapp.PurgeLocalChat(repo, "device", []string{"120@s.whatsapp.net"}, true, nil))
	require.FileExists(t, filepath.Join(dir, "group.jpg"))
}

func TestBoundedDeleteWaitsForConcurrentWriter(t *testing.T) {
	db, err := sql.Open(sqlite.DriverName, sqlite.FormatChatStorageURI("file:"+filepath.Join(t.TempDir(), "chat.db"), true, true))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(5)
	repo := &SQLiteRepository{db: db}
	require.NoError(t, repo.InitializeSchema())
	bound := time.Now().Truncate(time.Second)
	require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{ID: "old", DeviceID: "device", ChatJID: "chat", Content: "old", Timestamp: bound}))
	writer, err := db.Begin()
	require.NoError(t, err)
	defer writer.Rollback()
	_, err = writer.Exec("INSERT INTO messages(id,device_id,chat_jid,content,timestamp,sender) VALUES (?,?,?,?,?,'sender')", "new", "device", "chat", "new", bound.Add(time.Second))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { _, err := repo.DeleteChatByDeviceBefore("device", bound, "chat"); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for db.Stats().InUse < 2 {
		select {
		case err := <-done:
			t.Fatalf("purge did not wait for writer: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("purge did not acquire a second connection")
		}
		runtime.Gosched()
	}
	require.NoError(t, writer.Commit())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("purge did not finish")
	}
	var id string
	require.NoError(t, db.QueryRow("SELECT id FROM messages").Scan(&id))
	require.Equal(t, "new", id)
}

func TestBoundedDeleteOrphanChatwootLinksWithSurvivors(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	bound := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	seedDeleteChat(t, repo, "device", "chat", "old")
	seedDeleteChat(t, repo, "device", "chat", "survivor")
	_, err := repo.db.Exec("UPDATE messages SET timestamp = ? WHERE id = ?", bound, "old")
	require.NoError(t, err)
	_, err = repo.db.Exec("UPDATE messages SET timestamp = ? WHERE id = ?", bound.Add(time.Second), "survivor")
	require.NoError(t, err)
	for _, tc := range []struct {
		id, device, chat string
		ts               time.Time
	}{
		{"before", "device", "chat", bound.Add(-time.Second).In(time.FixedZone("JST", 9*3600))},
		{"at", "device", "chat", bound.In(time.FixedZone("PET", -5*3600))},
		{"after", "device", "chat", bound.Add(time.Second)},
		{"other-device", "other", "chat", bound},
		{"other-chat", "device", "other", bound},
	} {
		_, err := repo.db.Exec("INSERT INTO chatwoot_message_links(device_id,wa_chat_jid,wa_message_id,created_at) VALUES (?,?,?,?)", tc.device, tc.chat, tc.id, tc.ts)
		require.NoError(t, err)
	}
	removed, err := repo.DeleteChatByDeviceBefore("device", bound, "chat")
	require.NoError(t, err)
	require.False(t, removed)
	rows, err := repo.db.Query("SELECT wa_message_id FROM chatwoot_message_links ORDER BY wa_message_id")
	require.NoError(t, err)
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"after", "other-chat", "other-device", "survivor"}, ids)
}

func TestBoundedDeleteChunksMessageIDs(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	bound := time.Now().Truncate(time.Second)
	require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{DeviceID: "device", JID: "chat"}))
	tx, err := repo.db.Begin()
	require.NoError(t, err)
	defer tx.Rollback()
	for i := 0; i < 1002; i++ {
		id := fmt.Sprintf("m%d", i)
		ts := bound
		if i == 1001 {
			ts = bound.Add(time.Second)
		}
		_, err = tx.Exec("INSERT INTO messages(device_id,chat_jid,id,content,timestamp,sender) VALUES (?,?,?,'hello',?,'sender')", "device", "chat", id, ts)
		require.NoError(t, err)
		for _, query := range []string{
			"INSERT INTO message_reactions(device_id,chat_jid,message_id,reactor_jid,reaction_timestamp) VALUES (?,?,?,'sender',CURRENT_TIMESTAMP)",
			"INSERT INTO message_edits(device_id,chat_jid,original_message_id,edit_event_id,edited_at) VALUES (?,?,?,'edit',CURRENT_TIMESTAMP)",
			"INSERT INTO poll_definitions(device_id,chat_jid,poll_message_id) VALUES (?,?,?)",
			"INSERT INTO chatwoot_message_links(device_id,wa_chat_jid,wa_message_id) VALUES (?,?,?)",
		} {
			_, err = tx.Exec(query, "device", "chat", id)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tx.Commit())
	removed, err := repo.DeleteChatByDeviceBefore("device", bound, "chat")
	require.NoError(t, err)
	require.False(t, removed)
	for _, table := range deletedChatTables {
		var count int
		require.NoError(t, repo.db.QueryRow(table.totalCount).Scan(&count))
		require.Equal(t, 1, count, table.name)
	}
}

func TestGetLatestMessageByDeviceUsesInstantAcrossZonesAndAliases(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	device, pn, lid := "device-a", "628111@s.whatsapp.net", "999111@lid"
	jst, pet := time.FixedZone("JST", 9*3600), time.FixedZone("PET", -5*3600)
	store := func(device, jid, id string, ts time.Time) {
		t.Helper()
		require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{DeviceID: device, ChatJID: jid, ID: id, Content: "hello", Timestamp: ts, Sender: "999111@lid"}))
	}
	// Twenty +09:00 rows (14:00..14:19 UTC) and three UTC rows sort ahead of the newer -05:00 row as text.
	for i := range 20 {
		store(device, pn, fmt.Sprintf("jst-%02d", i), time.Date(2026, 11, 1, 23, i, 0, 0, jst))
	}
	for i := range 3 {
		store(device, pn, fmt.Sprintf("utc-%d", i), time.Date(2026, 11, 1, 20, i, 0, 0, time.UTC))
	}
	store(device, pn, "pet-old", time.Date(2026, 11, 1, 9, 0, 0, 0, pet))
	newest := time.Date(2026, 11, 1, 18, 30, 0, 0, pet)
	store(device, lid, "newest", newest)
	// Newer rows of another device or chat are out of scope.
	store("device-b", pn, "other-device", newest.Add(time.Hour))
	store(device, "628222@s.whatsapp.net", "other-chat", newest.Add(time.Hour))

	sample, err := repo.GetMessages(&domainChatStorage.MessageFilter{DeviceID: device, ChatJID: lid})
	require.NoError(t, err)
	require.Len(t, sample, 1)
	top, err := repo.GetMessages(&domainChatStorage.MessageFilter{DeviceID: device, ChatJID: pn, Limit: 20})
	require.NoError(t, err)
	require.Len(t, top, 20)
	for _, m := range top {
		require.True(t, strings.HasPrefix(m.ID, "jst-"), "text order puts %s in the top 20", m.ID)
	}

	got, err := repo.GetLatestMessageByDevice(device, pn, lid)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "newest", got.ID)
	require.Equal(t, lid, got.ChatJID)
	require.Equal(t, newest.Unix(), got.Timestamp.Unix())
	require.False(t, got.IsFromMe)
	require.Equal(t, "999111@lid", got.Sender)

	// Same second: the later stored row wins.
	store(device, pn, "same-second", newest.Add(500*time.Millisecond).UTC())
	got, err = repo.GetLatestMessageByDevice(device, pn, lid)
	require.NoError(t, err)
	require.Equal(t, "same-second", got.ID)

	none, err := repo.GetLatestMessageByDevice(device, "absent@s.whatsapp.net")
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = repo.GetLatestMessageByDevice("", pn)
	require.Error(t, err)

	_, err = repo.db.Exec("UPDATE messages SET timestamp = 'not a time' WHERE id = ?", "pet-old")
	require.NoError(t, err)
	_, err = repo.GetLatestMessageByDevice(device, pn, lid)
	require.Error(t, err, "an unreadable timestamp must fail closed")
}
