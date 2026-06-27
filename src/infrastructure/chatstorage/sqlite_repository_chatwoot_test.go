package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func TestSQLiteRepositoryStoresAndLooksUpChatwootMessageLinks(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC)

	link := &domainChatStorage.ChatwootMessageLink{
		DeviceID:                     "device-a@s.whatsapp.net",
		WhatsAppMessageID:            "wa-1",
		WhatsAppChatJID:              "628123456789@s.whatsapp.net",
		ChatwootMessageID:            101,
		ChatwootConversationID:       202,
		ChatwootInboxID:              303,
		ChatwootContactInboxSourceID: "628123456789@s.whatsapp.net",
		SourceID:                     "WAID:wa-1",
		Direction:                    "incoming",
		IsRead:                       false,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}

	if err := repo.UpsertChatwootMessageLink(link); err != nil {
		t.Fatalf("upsert link: %v", err)
	}

	byWA, err := repo.GetChatwootMessageLinkByWhatsAppID(link.DeviceID, link.WhatsAppMessageID)
	if err != nil {
		t.Fatalf("lookup by whatsapp id: %v", err)
	}
	if byWA == nil {
		t.Fatal("expected link by whatsapp id")
	}
	if byWA.ChatwootMessageID != 101 || byWA.ChatwootConversationID != 202 || byWA.SourceID != "WAID:wa-1" {
		t.Fatalf("unexpected link by whatsapp id: %+v", byWA)
	}

	byCW, err := repo.GetChatwootMessageLinkByChatwootID(link.DeviceID, link.ChatwootMessageID)
	if err != nil {
		t.Fatalf("lookup by chatwoot id: %v", err)
	}
	if byCW == nil {
		t.Fatal("expected link by chatwoot id")
	}
	if byCW.WhatsAppMessageID != "wa-1" || byCW.WhatsAppChatJID != link.WhatsAppChatJID {
		t.Fatalf("unexpected link by chatwoot id: %+v", byCW)
	}

	link.ChatwootConversationID = 404
	link.IsRead = true
	if err := repo.UpsertChatwootMessageLink(link); err != nil {
		t.Fatalf("update link: %v", err)
	}

	updated, err := repo.GetChatwootMessageLinkByWhatsAppID(link.DeviceID, link.WhatsAppMessageID)
	if err != nil {
		t.Fatalf("lookup updated link: %v", err)
	}
	if updated.ChatwootConversationID != 404 || !updated.IsRead {
		t.Fatalf("expected updated link, got %+v", updated)
	}
}

func TestSQLiteRepositoryChatwootMessageLinksAreDeviceScoped(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	if err := repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID:          "device-a@s.whatsapp.net",
		WhatsAppMessageID: "same-wa-id",
		WhatsAppChatJID:   "628111111111@s.whatsapp.net",
		ChatwootMessageID: 10,
		SourceID:          "WAID:same-wa-id",
		Direction:         "incoming",
	}); err != nil {
		t.Fatalf("upsert device A link: %v", err)
	}
	if err := repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID:          "device-b@s.whatsapp.net",
		WhatsAppMessageID: "same-wa-id",
		WhatsAppChatJID:   "628222222222@s.whatsapp.net",
		ChatwootMessageID: 20,
		SourceID:          "WAID:same-wa-id",
		Direction:         "incoming",
	}); err != nil {
		t.Fatalf("upsert device B link: %v", err)
	}

	linkA, err := repo.GetChatwootMessageLinkByWhatsAppID("device-a@s.whatsapp.net", "same-wa-id")
	if err != nil {
		t.Fatalf("lookup device A: %v", err)
	}
	linkB, err := repo.GetChatwootMessageLinkByWhatsAppID("device-b@s.whatsapp.net", "same-wa-id")
	if err != nil {
		t.Fatalf("lookup device B: %v", err)
	}

	if linkA.ChatwootMessageID != 10 {
		t.Fatalf("device A chatwoot id = %d, want 10", linkA.ChatwootMessageID)
	}
	if linkB.ChatwootMessageID != 20 {
		t.Fatalf("device B chatwoot id = %d, want 20", linkB.ChatwootMessageID)
	}

	if err := repo.DeleteDeviceData("device-a@s.whatsapp.net"); err != nil {
		t.Fatalf("delete device A data: %v", err)
	}
	linkA, err = repo.GetChatwootMessageLinkByWhatsAppID("device-a@s.whatsapp.net", "same-wa-id")
	if err != nil {
		t.Fatalf("lookup deleted device A: %v", err)
	}
	if linkA != nil {
		t.Fatalf("expected device A link to be deleted, got %+v", linkA)
	}

	linkB, err = repo.GetChatwootMessageLinkByWhatsAppID("device-b@s.whatsapp.net", "same-wa-id")
	if err != nil {
		t.Fatalf("lookup remaining device B: %v", err)
	}
	if linkB == nil || linkB.ChatwootMessageID != 20 {
		t.Fatalf("expected device B link to remain, got %+v", linkB)
	}
}

func TestSQLiteRepositoryGetsLatestUnreadIncomingChatwootMessageLinkByChat(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"

	links := []*domainChatStorage.ChatwootMessageLink{
		{
			DeviceID:          deviceID,
			WhatsAppMessageID: "old-unread",
			WhatsAppChatJID:   chatJID,
			ChatwootMessageID: 11,
			Direction:         "incoming",
			IsRead:            false,
			UpdatedAt:         time.Date(2026, time.June, 6, 9, 0, 0, 0, time.UTC),
		},
		{
			DeviceID:          deviceID,
			WhatsAppMessageID: "latest-unread",
			WhatsAppChatJID:   chatJID,
			ChatwootMessageID: 12,
			Direction:         "incoming",
			IsRead:            false,
			UpdatedAt:         time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
		},
		{
			DeviceID:          deviceID,
			WhatsAppMessageID: "outgoing",
			WhatsAppChatJID:   chatJID,
			ChatwootMessageID: 13,
			Direction:         "outgoing",
			IsRead:            false,
			UpdatedAt:         time.Date(2026, time.June, 6, 11, 0, 0, 0, time.UTC),
		},
		{
			DeviceID:          deviceID,
			WhatsAppMessageID: "read",
			WhatsAppChatJID:   chatJID,
			ChatwootMessageID: 14,
			Direction:         "incoming",
			IsRead:            true,
			UpdatedAt:         time.Date(2026, time.June, 6, 12, 0, 0, 0, time.UTC),
		},
	}
	for _, link := range links {
		if err := repo.UpsertChatwootMessageLink(link); err != nil {
			t.Fatalf("upsert %s: %v", link.WhatsAppMessageID, err)
		}
	}

	link, err := repo.GetLatestUnreadChatwootMessageLinkByChat(deviceID, chatJID)
	if err != nil {
		t.Fatalf("lookup latest unread: %v", err)
	}
	if link == nil || link.WhatsAppMessageID != "latest-unread" {
		t.Fatalf("latest unread = %+v, want latest-unread", link)
	}

	link.IsRead = true
	if err := repo.UpsertChatwootMessageLink(link); err != nil {
		t.Fatalf("mark latest read: %v", err)
	}
	next, err := repo.GetLatestUnreadChatwootMessageLinkByChat(deviceID, chatJID)
	if err != nil {
		t.Fatalf("lookup next unread: %v", err)
	}
	if next == nil || next.WhatsAppMessageID != "old-unread" {
		t.Fatalf("next unread = %+v, want old-unread", next)
	}
}

func TestSQLiteRepositoryGetsLatestChatwootMessageLinkByConversation(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	conversationID := 202

	links := []*domainChatStorage.ChatwootMessageLink{
		{
			DeviceID:               "device-a@s.whatsapp.net",
			WhatsAppMessageID:      "old",
			WhatsAppChatJID:        "628111111111@s.whatsapp.net",
			ChatwootMessageID:      11,
			ChatwootConversationID: conversationID,
			UpdatedAt:              time.Date(2026, time.June, 6, 9, 0, 0, 0, time.UTC),
		},
		{
			DeviceID:               "device-b@s.whatsapp.net",
			WhatsAppMessageID:      "latest",
			WhatsAppChatJID:        "628222222222@s.whatsapp.net",
			ChatwootMessageID:      12,
			ChatwootConversationID: conversationID,
			UpdatedAt:              time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
		},
		{
			DeviceID:               "device-c@s.whatsapp.net",
			WhatsAppMessageID:      "other-conversation",
			WhatsAppChatJID:        "628333333333@s.whatsapp.net",
			ChatwootMessageID:      13,
			ChatwootConversationID: 303,
			UpdatedAt:              time.Date(2026, time.June, 6, 11, 0, 0, 0, time.UTC),
		},
	}
	for _, link := range links {
		if err := repo.UpsertChatwootMessageLink(link); err != nil {
			t.Fatalf("upsert %s: %v", link.WhatsAppMessageID, err)
		}
	}

	link, err := repo.GetLatestChatwootMessageLinkByConversation(conversationID)
	if err != nil {
		t.Fatalf("lookup latest conversation link: %v", err)
	}
	if link == nil || link.WhatsAppMessageID != "latest" || link.DeviceID != "device-b@s.whatsapp.net" {
		t.Fatalf("latest conversation link = %+v, want device-b/latest", link)
	}
}

func TestSQLiteRepositoryChatwootForwardQueueLifecycle(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC)

	err := repo.EnqueueChatwootForwardEvent(&domainChatStorage.ChatwootForwardEvent{
		DeviceID:          "device-a@s.whatsapp.net",
		EventName:         "message",
		WhatsAppMessageID: "wa-queue-1",
		PayloadJSON:       `{"payload":{"id":"wa-queue-1"}}`,
		LastError:         "chatwoot down",
		NextAttemptAt:     now,
	})
	if err != nil {
		t.Fatalf("enqueue retry: %v", err)
	}

	due, err := repo.ListDueChatwootForwardEvents(now.Add(-time.Second), 10)
	if err != nil {
		t.Fatalf("list early due events: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("early due len = %d, want 0", len(due))
	}

	due, err = repo.ListDueChatwootForwardEvents(now, 10)
	if err != nil {
		t.Fatalf("list due events: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("due len = %d, want 1", len(due))
	}
	if due[0].DeviceID != "device-a@s.whatsapp.net" || due[0].WhatsAppMessageID != "wa-queue-1" || due[0].Attempts != 0 {
		t.Fatalf("unexpected due event: %+v", due[0])
	}

	nextAttempt := now.Add(2 * time.Minute)
	if err := repo.MarkChatwootForwardEventFailed(due[0].ID, "still down", nextAttempt); err != nil {
		t.Fatalf("mark retry failed: %v", err)
	}

	due, err = repo.ListDueChatwootForwardEvents(nextAttempt.Add(-time.Second), 10)
	if err != nil {
		t.Fatalf("list before next attempt: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due before next len = %d, want 0", len(due))
	}

	due, err = repo.ListDueChatwootForwardEvents(nextAttempt, 10)
	if err != nil {
		t.Fatalf("list next due events: %v", err)
	}
	if len(due) != 1 || due[0].Attempts != 1 || due[0].LastError != "still down" {
		t.Fatalf("unexpected retried event: %+v", due)
	}

	if err := repo.MarkChatwootForwardEventDone(due[0].ID); err != nil {
		t.Fatalf("mark retry done: %v", err)
	}
	due, err = repo.ListDueChatwootForwardEvents(nextAttempt.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("list after done: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("due after done len = %d, want 0", len(due))
	}
}
