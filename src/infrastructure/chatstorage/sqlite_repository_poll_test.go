package chatstorage

import (
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
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

	if err := repo.UpsertPollDefinition(definition); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
	got, err := repo.GetPollDefinition(definition.DeviceID, definition.ChatJID, definition.PollMessageID)
	if err != nil {
		t.Fatalf("GetPollDefinition: %v", err)
	}
	if got == nil || got.Question != "Lunch?" || got.Version != "v3" || got.SelectableOptionCount != 1 {
		t.Fatalf("unexpected definition: %+v", got)
	}
	if len(got.Options) != 2 || got.Options[0].Name != "Pizza" || got.Options[1].Hash != "hash-sushi" {
		t.Fatalf("options not preserved in order: %+v", got.Options)
	}

	other, err := repo.GetPollDefinition("device-b@s.whatsapp.net", definition.ChatJID, definition.PollMessageID)
	if err != nil {
		t.Fatalf("GetPollDefinition other device: %v", err)
	}
	if other != nil {
		t.Fatalf("definition leaked across devices: %+v", other)
	}
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
	if err := repo.UpsertPollDefinition(definition); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
	option := domainChatStorage.PollOption{Name: "Sushi", Hash: "hash-sushi"}
	if err := repo.AppendPollOption(definition.DeviceID, definition.ChatJID, definition.PollMessageID, option); err != nil {
		t.Fatalf("AppendPollOption first: %v", err)
	}
	if err := repo.AppendPollOption(definition.DeviceID, definition.ChatJID, definition.PollMessageID, option); err != nil {
		t.Fatalf("AppendPollOption duplicate: %v", err)
	}

	got, err := repo.GetPollDefinition(definition.DeviceID, definition.ChatJID, definition.PollMessageID)
	if err != nil {
		t.Fatalf("GetPollDefinition: %v", err)
	}
	if len(got.Options) != 2 || got.Options[0].Name != "Pizza" || got.Options[1].Name != "Sushi" {
		t.Fatalf("unexpected options: %+v", got.Options)
	}
}

func TestGetPollDefinitionRejectsMalformedOptionsJSON(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	definition := &domainChatStorage.PollDefinition{
		DeviceID: "device-a", ChatJID: "chat-a", PollMessageID: "poll-bad-json", Question: "Q",
	}
	if err := repo.UpsertPollDefinition(definition); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
	if _, err := repo.db.Exec(`UPDATE poll_definitions SET options_json = ? WHERE device_id = ? AND chat_jid = ? AND poll_message_id = ?`,
		"{", definition.DeviceID, definition.ChatJID, definition.PollMessageID); err != nil {
		t.Fatalf("corrupt options_json: %v", err)
	}
	if _, err := repo.GetPollDefinition(definition.DeviceID, definition.ChatJID, definition.PollMessageID); err == nil {
		t.Fatal("expected malformed options JSON to return an error")
	}
}

func TestPollDefinitionsFollowCleanupPaths(t *testing.T) {
	t.Run("delete message", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		storePollDefinitionForCleanup(t, repo, "device-a", "chat-a", "poll-a")
		if err := repo.DeleteMessageByDevice("device-a", "poll-a", "chat-a"); err != nil {
			t.Fatalf("DeleteMessageByDevice: %v", err)
		}
		assertPollDefinitionMissing(t, repo, "device-a", "chat-a", "poll-a")
	})

	t.Run("delete chat", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		storePollDefinitionForCleanup(t, repo, "device-a", "chat-a", "poll-a")
		if err := repo.DeleteChatByDevice("device-a", "chat-a"); err != nil {
			t.Fatalf("DeleteChatByDevice: %v", err)
		}
		assertPollDefinitionMissing(t, repo, "device-a", "chat-a", "poll-a")
	})

	t.Run("delete device", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		storePollDefinitionForCleanup(t, repo, "device-a", "chat-a", "poll-a")
		if err := repo.DeleteDeviceData("device-a"); err != nil {
			t.Fatalf("DeleteDeviceData: %v", err)
		}
		assertPollDefinitionMissing(t, repo, "device-a", "chat-a", "poll-a")
	})

	t.Run("truncate", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		storePollDefinitionForCleanup(t, repo, "device-a", "chat-a", "poll-a")
		if err := repo.TruncateAllChats(); err != nil {
			t.Fatalf("TruncateAllChats: %v", err)
		}
		assertPollDefinitionMissing(t, repo, "device-a", "chat-a", "poll-a")
	})
}

func storePollDefinitionForCleanup(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, pollID string) {
	t.Helper()
	if err := repo.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: deviceID, ChatJID: chatJID, PollMessageID: pollID, Question: "Q",
	}); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
}

func assertPollDefinitionMissing(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, pollID string) {
	t.Helper()
	got, err := repo.GetPollDefinition(deviceID, chatJID, pollID)
	if err != nil {
		t.Fatalf("GetPollDefinition: %v", err)
	}
	if got != nil {
		t.Fatalf("poll definition still exists: %+v", got)
	}
}
