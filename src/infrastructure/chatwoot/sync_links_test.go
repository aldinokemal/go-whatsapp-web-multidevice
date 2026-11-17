package chatwoot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot/pgimport"
)

type chatwootSyncLinkRepo struct {
	domainChatStorage.IChatStorageRepository
	links map[string]*domainChatStorage.ChatwootMessageLink
}

func newChatwootSyncLinkTestRepo() *chatwootSyncLinkRepo {
	return &chatwootSyncLinkRepo{links: make(map[string]*domainChatStorage.ChatwootMessageLink)}
}

func chatwootSyncLinkKey(deviceID, waMessageID string) string {
	return deviceID + "\x00" + waMessageID
}

func (r *chatwootSyncLinkRepo) UpsertChatwootMessageLink(link *domainChatStorage.ChatwootMessageLink) error {
	cloned := *link
	r.links[chatwootSyncLinkKey(link.DeviceID, link.WhatsAppMessageID)] = &cloned
	return nil
}

func (r *chatwootSyncLinkRepo) GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*domainChatStorage.ChatwootMessageLink, error) {
	link := r.links[chatwootSyncLinkKey(deviceID, waMessageID)]
	if link == nil {
		return nil, nil
	}
	cloned := *link
	return &cloned, nil
}

func TestSyncMessageSkipsExistingChatwootLink(t *testing.T) {
	repo := newChatwootSyncLinkTestRepo()
	msg := &domainChatStorage.Message{
		ID:        "wa-existing",
		DeviceID:  "device-a@s.whatsapp.net",
		ChatJID:   "628123456789@s.whatsapp.net",
		Content:   "already synced",
		Timestamp: time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
	}
	if err := repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID:          msg.DeviceID,
		WhatsAppMessageID: msg.ID,
		WhatsAppChatJID:   msg.ChatJID,
		ChatwootMessageID: 777,
		SourceID:          "WAID:" + msg.ID,
		Direction:         "incoming",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	svc := NewSyncService(&Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}, repo)

	if err := svc.syncMessage(context.Background(), 42, msg, nil, SyncOptions{}, false); err != nil {
		t.Fatalf("syncMessage: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 for existing link", got)
	}
}

func TestSyncMessageStoresChatwootLinkAfterCreate(t *testing.T) {
	repo := newChatwootSyncLinkTestRepo()
	msg := &domainChatStorage.Message{
		ID:        "wa-new",
		DeviceID:  "device-a@s.whatsapp.net",
		ChatJID:   "628123456789@s.whatsapp.net",
		Content:   "new sync",
		Timestamp: time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/conversations/42/messages" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":888}`))
	}))
	defer server.Close()

	svc := NewSyncService(&Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}, repo)

	if err := svc.syncMessage(context.Background(), 42, msg, nil, SyncOptions{}, false); err != nil {
		t.Fatalf("syncMessage: %v", err)
	}

	link, err := repo.GetChatwootMessageLinkByWhatsAppID(msg.DeviceID, msg.ID)
	if err != nil {
		t.Fatalf("lookup link: %v", err)
	}
	if link == nil {
		t.Fatal("expected stored chatwoot message link")
	}
	if link.ChatwootMessageID != 888 || link.ChatwootConversationID != 42 || link.SourceID != "WAID:wa-new" {
		t.Fatalf("unexpected link: %+v", link)
	}
}

func TestSyncMessageWithRequiredMediaDoesNotCreatePlaceholderWhenDownloadFails(t *testing.T) {
	repo := newChatwootSyncLinkTestRepo()
	msg := &domainChatStorage.Message{
		ID:        "wa-media",
		DeviceID:  "device-a@s.whatsapp.net",
		ChatJID:   "628123456789@s.whatsapp.net",
		MediaType: "image",
		URL:       "https://mmg.whatsapp.net/file",
		MediaKey:  []byte("key"),
		Timestamp: time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":888}`))
	}))
	defer server.Close()

	svc := NewSyncService(&Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}, repo)

	err := svc.syncMessageWithOptions(context.Background(), 42, msg, nil, SyncOptions{IncludeMedia: true}, false, true)
	if err == nil {
		t.Fatal("syncMessageWithOptions error = nil, want media download failure")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 when required media cannot be attached", got)
	}
}

func TestStoreChatwootImportLinksPersistsReturnedLinks(t *testing.T) {
	repo := newChatwootSyncLinkTestRepo()
	svc := NewSyncService(nil, repo)

	result := &pgimport.ImportResult{
		Links: []domainChatStorage.ChatwootMessageLink{
			{
				DeviceID:               "device-a@s.whatsapp.net",
				WhatsAppMessageID:      "wa-imported",
				WhatsAppChatJID:        "628123456789@s.whatsapp.net",
				ChatwootMessageID:      909,
				ChatwootConversationID: 808,
				ChatwootInboxID:        707,
				SourceID:               "WAID:wa-imported",
				Direction:              "incoming",
			},
		},
	}

	if err := svc.storeChatwootImportLinks(result); err != nil {
		t.Fatalf("storeChatwootImportLinks: %v", err)
	}

	link, err := repo.GetChatwootMessageLinkByWhatsAppID("device-a@s.whatsapp.net", "wa-imported")
	if err != nil {
		t.Fatalf("lookup link: %v", err)
	}
	if link == nil || link.ChatwootMessageID != 909 {
		t.Fatalf("expected imported link to be stored, got %+v", link)
	}
}
