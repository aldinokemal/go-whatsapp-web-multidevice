package chatstorage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const replyContextJSON = `{"replied_to_id":"PARENT-1"}`

func contextTestMessage(deviceID, chatJID, id, metadata string) *domainChatStorage.Message {
	return &domainChatStorage.Message{ID: id, ChatJID: chatJID, DeviceID: deviceID,
		Sender: "sender@s.whatsapp.net", Content: "reply content", Timestamp: time.Now().UTC(), ContextMetadata: metadata}
}

// Exercise every projection paired with scanMessage, including both scoped
// single-row lookups that broke quoting and editing in the original PR.
func TestContextMetadataAllMessageProjections(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stored any
		want   string
	}{
		{"reply JSON", replyContextJSON, replyContextJSON}, {"empty", "", ""}, {"nullable legacy row", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			require.NoError(t, repo.StoreMessage(contextTestMessage("device-a", "chat@s.whatsapp.net", "reply", "")))
			_, err := repo.db.Exec(`UPDATE messages SET context_metadata = ?`, tc.stored)
			require.NoError(t, err)
			reads := map[string]func() (*domainChatStorage.Message, error){
				"global ID": func() (*domainChatStorage.Message, error) { return repo.GetMessageByID("reply") },
				"device ID": func() (*domainChatStorage.Message, error) { return repo.GetMessageByIDAndDevice("device-a", "reply") },
				"oldest history anchor": func() (*domainChatStorage.Message, error) {
					return repo.GetOldestMessageByDevice("device-a", "chat@s.whatsapp.net")
				},
				"internal edit lookup": func() (*domainChatStorage.Message, error) {
					return repo.getMessageByDeviceAndChatIDAndMessageID("device-a", "chat@s.whatsapp.net", "reply")
				},
				"device chat ID": func() (*domainChatStorage.Message, error) {
					return repo.GetMessageByIDChatAndDevice("device-a", "chat@s.whatsapp.net", "reply")
				},
			}
			for name, read := range reads {
				t.Run(name, func(t *testing.T) {
					got, err := read()
					require.NoError(t, err)
					require.NotNil(t, got)
					require.Equal(t, tc.want, got.ContextMetadata)
				})
			}
			messages, err := repo.GetMessages(&domainChatStorage.MessageFilter{DeviceID: "device-a", ChatJID: "chat@s.whatsapp.net", Limit: 10})
			require.NoError(t, err)
			require.Len(t, messages, 1)
			require.Equal(t, tc.want, messages[0].ContextMetadata)
			messages, err = repo.SearchMessages("device-a", "chat@s.whatsapp.net", "reply", 10)
			require.NoError(t, err)
			require.Len(t, messages, 1)
			require.Equal(t, tc.want, messages[0].ContextMetadata)
		})
	}
}

func TestContextMetadataWritesPreserveOmittedContextAndIsolation(t *testing.T) {
	for _, writer := range []string{"single", "batch", "sent"} {
		t.Run(writer, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			write := func(message *domainChatStorage.Message) error {
				switch writer {
				case "batch":
					return repo.StoreMessagesBatch([]*domainChatStorage.Message{message})
				case "sent":
					return repo.storeSentMessagePreservingEdits(message)
				default:
					return repo.StoreMessage(message)
				}
			}
			const chat = "chat@s.whatsapp.net"
			// Identical message IDs across devices/chats must never share reply context.
			require.NoError(t, write(contextTestMessage("device-b", chat, "reply", `{"replied_to_id":"OTHER-DEVICE"}`)))
			require.NoError(t, write(contextTestMessage("device-a", "other@g.us", "reply", `{"replied_to_id":"OTHER-CHAT"}`)))
			for _, step := range []struct{ supplied, want string }{
				{"", ""}, {replyContextJSON, replyContextJSON}, {"", replyContextJSON},
				{`{"replied_to_id":"UPDATED"}`, `{"replied_to_id":"UPDATED"}`},
			} {
				require.NoError(t, write(contextTestMessage("device-a", chat, "reply", step.supplied)))
				got, err := repo.GetMessageByIDChatAndDevice("device-a", chat, "reply")
				require.NoError(t, err)
				require.NotNil(t, got)
				require.Equal(t, step.want, got.ContextMetadata)
			}
			for _, scope := range []struct{ device, chat, want string }{
				{"device-b", chat, `{"replied_to_id":"OTHER-DEVICE"}`}, {"device-a", "other@g.us", `{"replied_to_id":"OTHER-CHAT"}`},
			} {
				got, err := repo.GetMessageByIDChatAndDevice(scope.device, scope.chat, "reply")
				require.NoError(t, err)
				require.Equal(t, scope.want, got.ContextMetadata)
			}
			missing, err := repo.GetMessageByIDAndDevice("absent-device", "reply")
			require.NoError(t, err)
			require.Nil(t, missing)
		})
	}
}

func TestContextMetadataMigrationFromExistingDatabase(t *testing.T) {
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "legacy.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &SQLiteRepository{db: db}
	_, err = repo.getSchemaVersion()
	require.NoError(t, err)
	migrations := repo.getMigrations()
	require.GreaterOrEqual(t, len(migrations), 49)
	// Construct current-main schema, including direct_path at migration 30.
	for i, migration := range migrations[:48] {
		require.NoError(t, repo.runMigration(migration, i+1))
	}
	_, err = db.Exec(`INSERT INTO messages (
  id, chat_jid, device_id, sender, content, timestamp, is_from_me,
  media_type, call_metadata, filename, url, direct_path, file_length, referral_metadata
 ) VALUES ('legacy', 'chat@s.whatsapp.net', 'device-a', 'sender', 'legacy content', ?, FALSE,
  '', '', '', '', '/old-media-path', 0, '')`, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, repo.InitializeSchema())
	require.NoError(t, repo.InitializeSchema(), "migration must be idempotent on restart")
	version, err := repo.getSchemaVersion()
	require.NoError(t, err)
	require.Equal(t, len(migrations), version)
	legacy, err := repo.GetMessageByIDAndDevice("device-a", "legacy")
	require.NoError(t, err)
	require.NotNil(t, legacy)
	require.Empty(t, legacy.ContextMetadata)
	require.Equal(t, "legacy content", legacy.Content)
	require.Equal(t, "/old-media-path", legacy.DirectPath)
	require.NoError(t, repo.StoreMessage(contextTestMessage("device-a", "chat@s.whatsapp.net", "new", replyContextJSON)))
	fresh, err := repo.GetMessageByIDAndDevice("device-a", "new")
	require.NoError(t, err)
	require.Equal(t, replyContextJSON, fresh.ContextMetadata)
}

func replyProto() *waE2E.Message {
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("reply content"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("PARENT-1")},
	}}
}

func TestContextMetadataLiveAndSentIngestion(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		for _, ingestion := range []string{"live", "sent"} {
			name := ingestion
			if wrapped {
				name += "/wrapped"
			}
			t.Run(name, func(t *testing.T) {
				repo := newTestSQLiteRepository(t)
				ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("device-a", nil, nil))
				msg := replyProto()
				if wrapped {
					msg = &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: msg}}
				}
				if ingestion == "sent" {
					require.NoError(t, repo.StoreSentMessageWithContext(ctx, "reply", "sender@s.whatsapp.net", "chat@s.whatsapp.net", "reply content", time.Now(), msg))
				} else {
					require.NoError(t, repo.CreateMessage(ctx, &events.Message{
						Info: types.MessageInfo{ID: "reply", Timestamp: time.Now(), MessageSource: types.MessageSource{
							Chat: types.NewJID("chat", types.DefaultUserServer), Sender: types.NewJID("sender", types.DefaultUserServer),
						}}, Message: msg,
					}))
				}
				got, err := repo.GetMessageByIDAndDevice("device-a", "reply")
				require.NoError(t, err)
				require.NotNil(t, got)
				require.Equal(t, replyContextJSON, got.ContextMetadata)
				// A later partial history replay must not erase the live/sent relationship.
				require.NoError(t, repo.StoreMessagesBatch([]*domainChatStorage.Message{contextTestMessage("device-a", "chat@s.whatsapp.net", "reply", "")}))
				got, err = repo.GetMessageByIDAndDevice("device-a", "reply")
				require.NoError(t, err)
				require.Equal(t, replyContextJSON, got.ContextMetadata)
			})
		}
	}
}

func TestContextMetadataSentEditOrdering(t *testing.T) {
	for _, editFirst := range []bool{false, true} {
		name := "edit after original"
		if editFirst {
			name = "edit before original"
		}
		t.Run(name, func(t *testing.T) {
			repo, ctx := orderTestRepo(t)
			now := time.Now().UTC()
			store := func() {
				require.NoError(t, repo.StoreSentMessageWithContext(ctx, orderMsgID, orderDevice, orderChatJID, "original", now, replyProto()))
			}
			edit := func() {
				require.NoError(t, repo.CreateMessage(ctx, editEvent(orderMsgID, "edited", now.Add(time.Second))))
			}
			if editFirst {
				edit()
				store()
			} else {
				store()
				edit()
			}
			got, err := repo.GetMessageByIDAndDevice(orderDevice, orderMsgID)
			require.NoError(t, err)
			require.Equal(t, "edited", got.Content)
			require.Equal(t, replyContextJSON, got.ContextMetadata)
			require.NoError(t, repo.StoreSentMessageWithContext(ctx, orderMsgID, orderDevice, orderChatJID, "original", now, nil))
			got, err = repo.GetMessageByIDAndDevice(orderDevice, orderMsgID)
			require.NoError(t, err)
			require.Equal(t, "edited", got.Content)
			require.Equal(t, replyContextJSON, got.ContextMetadata)
		})
	}
}

func TestContextMetadataEditCanSupplyMissingReply(t *testing.T) {
	for _, exists := range []bool{false, true} {
		name := "new row"
		if exists {
			name = "existing row"
		}
		t.Run(name, func(t *testing.T) {
			repo, ctx := orderTestRepo(t)
			if exists {
				require.NoError(t, repo.StoreMessage(contextTestMessage(orderDevice, orderChatJID, orderMsgID, "")))
			}
			evt := editEvent(orderMsgID, "edited", time.Now())
			evt.Message.ProtocolMessage.EditedMessage = replyProto()
			require.NoError(t, repo.CreateMessage(ctx, evt))
			got, err := repo.GetMessageByIDAndDevice(orderDevice, orderMsgID)
			require.NoError(t, err)
			require.Equal(t, replyContextJSON, got.ContextMetadata)
		})
	}
}
