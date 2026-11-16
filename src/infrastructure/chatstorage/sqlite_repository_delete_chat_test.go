package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
)

var deletedChatTables = []struct{ name, column string }{
	{"messages", "chat_jid"}, {"message_reactions", "chat_jid"}, {"message_edits", "chat_jid"},
	{"poll_definitions", "chat_jid"}, {"chatwoot_message_links", "wa_chat_jid"}, {"chats", "jid"},
}

func seedDeleteChat(t *testing.T, repo *SQLiteRepository, device, jid string) {
	t.Helper()
	id := "msg-" + jid
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
				require.NoError(t, repo.db.QueryRow("SELECT COUNT(*) FROM "+table.name+" WHERE device_id = ? AND "+table.column+" = ?", device, jid).Scan(&got))
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
		require.NoError(t, repo.db.QueryRow("SELECT COUNT(*) FROM "+table.name).Scan(&got))
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
