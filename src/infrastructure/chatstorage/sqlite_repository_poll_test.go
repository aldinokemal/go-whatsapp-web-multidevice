package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPollDefinitionRoundTripAndIsolation(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	definition := &domainChatStorage.PollDefinition{
		DeviceID:              "device-a@s.whatsapp.net",
		ChatJID:               "120363000000@g.us",
		PollMessageID:         "POLL-1",
		Question:              "Lunch?",
		SelectableOptionCount: 1,
		Version:               "v3",
		Options: []domainChatStorage.PollOption{
			{Name: "Pizza", Hash: "hash-pizza"},
			{Name: "Sushi", Hash: "hash-sushi"},
		},
	}

	require.NoError(t, repo.UpsertPollDefinition(definition))
	got, err := repo.GetPollDefinition(definition.DeviceID, definition.ChatJID, definition.PollMessageID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Lunch?", got.Question)
	assert.Equal(t, "v3", got.Version)
	assert.Equal(t, uint32(1), got.SelectableOptionCount)
	require.Len(t, got.Options, 2)
	assert.Equal(t, "Pizza", got.Options[0].Name)
	assert.Equal(t, "hash-sushi", got.Options[1].Hash)

	other, err := repo.GetPollDefinition("device-b@s.whatsapp.net", definition.ChatJID, definition.PollMessageID)
	require.NoError(t, err)
	assert.Nil(t, other, "definition leaked across devices")
}

func TestAppendPollOptionIsOrderedAndIdempotent(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	definition := &domainChatStorage.PollDefinition{
		DeviceID:      "device-a@s.whatsapp.net",
		ChatJID:       "120363000000@g.us",
		PollMessageID: "POLL-2",
		Question:      "Lunch?",
		Options:       []domainChatStorage.PollOption{{Name: "Pizza", Hash: "hash-pizza"}},
	}
	require.NoError(t, repo.UpsertPollDefinition(definition))
	option := domainChatStorage.PollOption{Name: "Sushi", Hash: "hash-sushi"}
	require.NoError(t, repo.AppendPollOption(definition.DeviceID, definition.ChatJID, definition.PollMessageID, option))
	require.NoError(t, repo.AppendPollOption(definition.DeviceID, definition.ChatJID, definition.PollMessageID, option))

	got, err := repo.GetPollDefinition(definition.DeviceID, definition.ChatJID, definition.PollMessageID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.Options, 2)
	assert.Equal(t, "Pizza", got.Options[0].Name)
	assert.Equal(t, "Sushi", got.Options[1].Name)
}

func TestGetPollDefinitionRejectsMalformedOptionsJSON(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	definition := &domainChatStorage.PollDefinition{
		DeviceID: "device-a", ChatJID: "chat-a", PollMessageID: "poll-bad-json", Question: "Q",
	}
	require.NoError(t, repo.UpsertPollDefinition(definition))
	_, err := repo.db.Exec(`UPDATE poll_definitions SET options_json = ? WHERE device_id = ? AND chat_jid = ? AND poll_message_id = ?`,
		"{", definition.DeviceID, definition.ChatJID, definition.PollMessageID)
	require.NoError(t, err)
	_, err = repo.GetPollDefinition(definition.DeviceID, definition.ChatJID, definition.PollMessageID)
	assert.Error(t, err, "expected malformed options JSON to return an error")
}

func TestUpsertPollDefinitionDoesNotReplaceNewerDefinition(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	newerTime := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	olderTime := newerTime.Add(-time.Hour)
	newer := &domainChatStorage.PollDefinition{
		DeviceID:      "device-a",
		ChatJID:       "chat-a",
		PollMessageID: "poll-monotonic",
		Question:      "Edited question",
		Options: []domainChatStorage.PollOption{
			{Name: "Original", Hash: "hash-original"},
			{Name: "Added live", Hash: "hash-added"},
		},
		UpdatedAt: newerTime,
	}
	replayedCreation := &domainChatStorage.PollDefinition{
		DeviceID:      newer.DeviceID,
		ChatJID:       newer.ChatJID,
		PollMessageID: newer.PollMessageID,
		Question:      "Original question",
		Options:       []domainChatStorage.PollOption{{Name: "Original", Hash: "hash-original"}},
		UpdatedAt:     olderTime,
	}

	require.NoError(t, repo.UpsertPollDefinition(newer))
	require.NoError(t, repo.UpsertPollDefinition(replayedCreation))
	got, err := repo.GetPollDefinition(newer.DeviceID, newer.ChatJID, newer.PollMessageID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Edited question", got.Question)
	assert.True(t, got.UpdatedAt.Equal(newerTime), "updated_at = %s, want %s", got.UpdatedAt, newerTime)
	require.Len(t, got.Options, 2)
	assert.Equal(t, "Added live", got.Options[1].Name)
}

func TestGetPollDefinitionByIDAndDeviceResolvesChatAliasOnlyWhenUnambiguous(t *testing.T) {
	t.Run("single matching poll", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
			DeviceID: "device-a", ChatJID: "628111@s.whatsapp.net", PollMessageID: "poll-alias", Question: "Q",
		}))
		got, err := repo.GetPollDefinitionByIDAndDevice("device-a", "poll-alias")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "628111@s.whatsapp.net", got.ChatJID)
	})

	t.Run("ambiguous message id", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		for _, chatJID := range []string{"chat-a", "chat-b"} {
			require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
				DeviceID: "device-a", ChatJID: chatJID, PollMessageID: "poll-duplicate", Question: "Q",
			}))
		}
		got, err := repo.GetPollDefinitionByIDAndDevice("device-a", "poll-duplicate")
		assert.Error(t, err)
		assert.Nil(t, got)
	})
}

func TestPollDefinitionsFollowCleanupPaths(t *testing.T) {
	tests := []struct {
		name    string
		cleanup func(repo *SQLiteRepository) error
	}{
		{
			name:    "delete message",
			cleanup: func(repo *SQLiteRepository) error { return repo.DeleteMessageByDevice("device-a", "poll-a", "chat-a") },
		},
		{
			name:    "delete chat",
			cleanup: func(repo *SQLiteRepository) error { return repo.DeleteChatByDevice("device-a", "chat-a") },
		},
		{
			name:    "delete device",
			cleanup: func(repo *SQLiteRepository) error { return repo.DeleteDeviceData("device-a") },
		},
		{
			name:    "truncate",
			cleanup: func(repo *SQLiteRepository) error { return repo.TruncateAllChats() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			require.NoError(t, repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
				DeviceID: "device-a", ChatJID: "chat-a", PollMessageID: "poll-a", Question: "Q",
			}))
			require.NoError(t, tt.cleanup(repo))
			got, err := repo.GetPollDefinition("device-a", "chat-a", "poll-a")
			require.NoError(t, err)
			assert.Nil(t, got, "poll definition should be cleaned up")
		})
	}
}
