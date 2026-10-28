package whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

// chatwootRequestLog is a Chatwoot stand-in that records every request and
// answers 200 with an empty object.
type chatwootRequestLog struct {
	mu   sync.Mutex
	seen []string
}

func (l *chatwootRequestLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	l.seen = append(l.seen, r.Method+" "+r.URL.Path)
	l.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (l *chatwootRequestLog) take() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := strings.Join(l.seen, ", ")
	l.seen = nil
	return out
}

// ignoreLinkRepo holds Chatwoot message links by WhatsApp message id; the rest
// of the repository is the embedded (nil) interface.
type ignoreLinkRepo struct {
	domainChatStorage.IChatStorageRepository
	links map[string]*domainChatStorage.ChatwootMessageLink
}

func (r *ignoreLinkRepo) GetChatwootMessageLinkByWhatsAppID(_, waMessageID string) (*domainChatStorage.ChatwootMessageLink, error) {
	link := r.links[waMessageID]
	if link == nil {
		return nil, nil
	}
	cloned := *link
	return &cloned, nil
}

func (r *ignoreLinkRepo) UpsertChatwootMessageLink(link *domainChatStorage.ChatwootMessageLink) error {
	cloned := *link
	r.links[link.WhatsAppMessageID] = &cloned
	return nil
}

// stubChatwootDevice points the forward path at srv with the given per-device
// ignore list.
func stubChatwootDevice(t *testing.T, srv *httptest.Server, deviceIgnore *[]string) {
	t.Helper()
	orig := getChatwootClientFn
	t.Cleanup(func() { getChatwootClientFn = orig })
	client := &chatwoot.Client{BaseURL: srv.URL, APIToken: "t", AccountID: 1, InboxID: 2, InboxIdentifier: "inbox-ident", HTTPClient: srv.Client()}
	getChatwootClientFn = func(deviceID string) (*chatwoot.ResolvedConfig, error) {
		return &chatwoot.ResolvedConfig{ConfigID: 1, DeviceID: deviceID, Client: client, IgnoreJids: *deviceIgnore}, nil
	}
}

// TestSyncPayloadToChatwootIgnoresResolvedLID covers ignore entries written as
// a LID when the LID resolved to a phone number: buildFromFields then carries
// the phone JID in chat_id/from and the LID only in chat_lid/from_lid.
func TestSyncPayloadToChatwootIgnoresResolvedLID(t *testing.T) {
	prevGlobal := config.ChatwootIgnoreJids
	t.Cleanup(func() { config.ChatwootIgnoreJids = prevGlobal })

	log := &chatwootRequestLog{}
	srv := httptest.NewServer(log)
	t.Cleanup(srv.Close)
	var deviceIgnore []string
	stubChatwootDevice(t, srv, &deviceIgnore)

	dm := map[string]any{"payload": map[string]any{
		"id": "wa-dm", "chat_id": "628111@s.whatsapp.net", "chat_lid": "123@lid",
		"from": "628111@s.whatsapp.net", "from_lid": "123@lid", "from_name": "Ana", "body": "hello",
	}}
	group := map[string]any{"payload": map[string]any{
		"id": "wa-group", "chat_id": "120363000000000001@g.us",
		"from": "628111@s.whatsapp.net", "from_lid": "123@lid", "from_name": "Ana", "body": "hello",
	}}

	tests := []struct {
		name        string
		global      []string
		device      []string
		payload     map[string]any
		wantIgnored bool
	}{
		{"device exact lid, resolved dm", nil, []string{"123@lid"}, dm, true},
		{"device lid wildcard, resolved dm", nil, []string{"@lid"}, dm, true},
		{"device exact lid, resolved group sender", nil, []string{"123@lid"}, group, true},
		{"device lid wildcard, resolved group sender", nil, []string{"@lid"}, group, true},
		{"global exact lid, resolved dm", []string{"123@lid"}, nil, dm, true},
		{"global lid wildcard, resolved group sender", []string{"@lid"}, nil, group, true},
		{"another lid does not match", []string{"999@lid"}, []string{"998@lid"}, dm, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.ChatwootIgnoreJids = tt.global
			deviceIgnore = tt.device
			log.take()
			_ = syncPayloadToChatwoot(context.Background(), tt.payload, "message", "dev", nil)
			seen := log.take()
			if tt.wantIgnored && seen != "" {
				t.Fatalf("ignored identity reached Chatwoot: %s", seen)
			}
			if !tt.wantIgnored && seen == "" {
				t.Fatal("a message that matches no entry must still be forwarded")
			}
		})
	}
}

// TestIgnoredChatKeepsReceiptsAndDeletesOfMirroredMessages pins the contract
// for a chat that was put on the ignore list after it was already linked to a
// Chatwoot conversation: nothing new is created for it (messages, edits,
// reactions, delete notes), but a read receipt or a delete-for-everyone of a
// message Chatwoot already holds is still applied, so the existing
// conversation does not keep showing stale state.
func TestIgnoredChatKeepsReceiptsAndDeletesOfMirroredMessages(t *testing.T) {
	prevDelete, prevNotes, prevGlobal := config.ChatwootMessageDelete, config.ChatwootForwardDeletes, config.ChatwootIgnoreJids
	t.Cleanup(func() {
		config.ChatwootMessageDelete, config.ChatwootForwardDeletes, config.ChatwootIgnoreJids = prevDelete, prevNotes, prevGlobal
	})
	config.ChatwootMessageDelete = true
	config.ChatwootForwardDeletes = true
	config.ChatwootIgnoreJids = nil

	const chat = "120363000000000001@g.us"
	log := &chatwootRequestLog{}
	srv := httptest.NewServer(log)
	t.Cleanup(srv.Close)
	deviceIgnore := []string{chat}
	stubChatwootDevice(t, srv, &deviceIgnore)

	repo := &ignoreLinkRepo{links: map[string]*domainChatStorage.ChatwootMessageLink{
		"wa-mirrored": {
			DeviceID: "dev", WhatsAppMessageID: "wa-mirrored", WhatsAppChatJID: chat,
			ChatwootMessageID: 9, ChatwootConversationID: 5, ChatwootAccountID: 1,
			ChatwootContactInboxSourceID: "src-1",
		},
	}}
	base := func(extra map[string]any) map[string]any {
		data := map[string]any{"chat_id": chat, "from": "628111@s.whatsapp.net", "from_name": "Ana"}
		for k, v := range extra {
			data[k] = v
		}
		return map[string]any{"payload": data}
	}
	run := func(event string, payload map[string]any) string {
		t.Helper()
		log.take()
		if err := syncPayloadToChatwoot(context.Background(), payload, event, "dev", repo); err != nil {
			t.Fatalf("%s: unexpected error %v", event, err)
		}
		return log.take()
	}

	// New content for the ignored chat never reaches Chatwoot.
	for event, payload := range map[string]map[string]any{
		"message":          base(map[string]any{"id": "wa-new", "body": "hello"}),
		"message.edited":   base(map[string]any{"id": "wa-edit", "original_message_id": "wa-mirrored", "body": "edited"}),
		"message.reaction": base(map[string]any{"id": "wa-react", "reacted_message_id": "wa-mirrored", "reaction": "👍"}),
		// Delete of a message Chatwoot never held: no hard delete is possible
		// and the tombstone note would be new content.
		"message.revoked": base(map[string]any{"id": "wa-rev", "revoked_message_id": "wa-never-mirrored"}),
	} {
		if seen := run(event, payload); seen != "" {
			t.Fatalf("%s for an ignored chat reached Chatwoot: %s", event, seen)
		}
	}
	// A receipt for a message Chatwoot never held has nothing to update.
	if seen := run("message.ack", base(map[string]any{"receipt_type": "read", "ids": []any{"wa-never-mirrored"}})); seen != "" {
		t.Fatalf("receipt for an unlinked message reached Chatwoot: %s", seen)
	}

	// Read receipt of an already mirrored message: last-seen is updated.
	seen := run("message.ack", base(map[string]any{"receipt_type": "read", "ids": []any{"wa-mirrored"}}))
	if want := "POST /public/api/v1/inboxes/inbox-ident/contacts/src-1/conversations/5/update_last_seen"; seen != want {
		t.Fatalf("read receipt of a mirrored message: got %q, want %q", seen, want)
	}
	if !repo.links["wa-mirrored"].IsRead {
		t.Fatal("the link must be marked read")
	}

	// Delete-for-everyone of an already mirrored message: removed in Chatwoot,
	// and no tombstone note is added.
	seen = run("message.revoked", base(map[string]any{"id": "wa-rev2", "revoked_message_id": "wa-mirrored"}))
	if want := "DELETE /api/v1/accounts/1/conversations/5/messages/9"; seen != want {
		t.Fatalf("delete of a mirrored message: got %q, want %q", seen, want)
	}

	// With hard delete switched off the only thing left would be the note,
	// which is new content and therefore suppressed.
	config.ChatwootMessageDelete = false
	if seen := run("message.revoked", base(map[string]any{"id": "wa-rev3", "revoked_message_id": "wa-mirrored"})); seen != "" {
		t.Fatalf("delete note for an ignored chat reached Chatwoot: %s", seen)
	}
}
