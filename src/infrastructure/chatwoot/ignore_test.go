package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

func TestShouldIgnoreMessageMatchesEveryForm(t *testing.T) {
	resolved := MessageIdentity{
		ChatJID:   "120363000000000001@g.us",
		SenderJID: "628111@s.whatsapp.net",
		SenderLID: "123@lid",
	}
	resolvedDM := MessageIdentity{
		ChatJID:   "628111@s.whatsapp.net",
		ChatLID:   "123@lid",
		SenderJID: "628111@s.whatsapp.net",
		SenderLID: "123@lid",
	}

	tests := []struct {
		name   string
		id     MessageIdentity
		ignore []string
		want   bool
	}{
		{"empty list", resolved, nil, false},
		{"exact chat", resolved, []string{"120363000000000001@g.us"}, true},
		{"group wildcard", resolved, []string{"@g.us"}, true},
		{"exact sender phone", resolved, []string{"628111@s.whatsapp.net"}, true},
		{"exact sender lid of a resolved sender", resolved, []string{"123@lid"}, true},
		{"lid wildcard on a resolved sender", resolved, []string{"@lid"}, true},
		{"exact chat lid of a resolved dm", resolvedDM, []string{"123@lid"}, true},
		{"unrelated entries", resolved, []string{"999@lid", "628999@s.whatsapp.net", "120363000000000002@g.us"}, false},
		{"blank entries never match", MessageIdentity{}, []string{"", " "}, false},
		{"lid wildcard without a known lid", MessageIdentity{ChatJID: "628111@s.whatsapp.net", SenderJID: "628111@s.whatsapp.net"}, []string{"@lid"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShouldIgnoreMessage(tt.id, tt.ignore); got != tt.want {
				t.Fatalf("ShouldIgnoreMessage(%+v, %v) = %v, want %v", tt.id, tt.ignore, got, tt.want)
			}
		})
	}
}

func TestEffectiveIgnoreJidsAddsDeviceListToGlobal(t *testing.T) {
	prev := config.ChatwootIgnoreJids
	config.ChatwootIgnoreJids = []string{"@g.us"}
	t.Cleanup(func() { config.ChatwootIgnoreJids = prev })

	got := EffectiveIgnoreJids([]string{"123@lid"})
	if len(got) != 2 || got[0] != "@g.us" || got[1] != "123@lid" {
		t.Fatalf("EffectiveIgnoreJids = %v, want global then device", got)
	}
	if len(config.ChatwootIgnoreJids) != 1 {
		t.Fatalf("global list was modified: %v", config.ChatwootIgnoreJids)
	}
	var none *ResolvedConfig
	if got := none.EffectiveIgnoreJids(); len(got) != 1 || got[0] != "@g.us" {
		t.Fatalf("nil config must yield the global list, got %v", got)
	}
}

// fakeLIDStore maps phone JIDs and LIDs both ways; the unused part of
// whatsmeow's LIDStore is the embedded (nil) interface.
type fakeLIDStore struct {
	store.LIDStore
	pnToLID map[string]string
	calls   int
	err     error
}

func (f *fakeLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	f.calls++
	if f.err != nil {
		return types.EmptyJID, f.err
	}
	for pn, l := range f.pnToLID {
		if l == lid.String() {
			return types.ParseJID(pn)
		}
	}
	return types.EmptyJID, nil
}

func (f *fakeLIDStore) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	f.calls++
	if f.err != nil {
		return types.EmptyJID, f.err
	}
	if l, ok := f.pnToLID[pn.String()]; ok {
		return types.ParseJID(l)
	}
	return types.EmptyJID, nil
}

func TestJIDFormResolver(t *testing.T) {
	lids := &fakeLIDStore{pnToLID: map[string]string{"628111@s.whatsapp.net": "123@lid"}}
	r := newJIDFormResolver(&whatsmeow.Client{Store: &store.Device{LIDs: lids}})
	ctx := context.Background()

	if pn, lid := r.forms(ctx, "628111@s.whatsapp.net"); pn != "628111@s.whatsapp.net" || lid != "123@lid" {
		t.Fatalf("phone -> (%q, %q)", pn, lid)
	}
	if pn, lid := r.forms(ctx, "123@lid"); pn != "628111@s.whatsapp.net" || lid != "123@lid" {
		t.Fatalf("lid -> (%q, %q)", pn, lid)
	}
	if pn, lid := r.forms(ctx, "628999@s.whatsapp.net"); pn != "628999@s.whatsapp.net" || lid != "" {
		t.Fatalf("unmapped phone -> (%q, %q)", pn, lid)
	}
	if pn, lid := r.forms(ctx, "120363000000000001@g.us"); pn != "120363000000000001@g.us" || lid != "" {
		t.Fatalf("group -> (%q, %q)", pn, lid)
	}
	before := lids.calls
	r.forms(ctx, "628111@s.whatsapp.net")
	if lids.calls != before {
		t.Fatal("a repeated lookup must be served from the cache")
	}

	// No client, or a failing mapping table: only the stored form is known.
	if pn, lid := newJIDFormResolver(nil).forms(ctx, "123@lid"); pn != "" || lid != "123@lid" {
		t.Fatalf("no client -> (%q, %q)", pn, lid)
	}
	failing := newJIDFormResolver(&whatsmeow.Client{Store: &store.Device{LIDs: &fakeLIDStore{err: errors.New("db down")}}})
	if pn, lid := failing.forms(ctx, "628111@s.whatsapp.net"); pn != "628111@s.whatsapp.net" || lid != "" {
		t.Fatalf("failing store -> (%q, %q)", pn, lid)
	}
}

// historyIgnoreRepo serves fixed chats and messages and records links.
type historyIgnoreRepo struct {
	*chatwootSyncLinkRepo
	chats    []*domainChatStorage.Chat
	messages map[string][]*domainChatStorage.Message
}

func (r *historyIgnoreRepo) GetChats(*domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	return r.chats, nil
}

func (r *historyIgnoreRepo) GetMessages(f *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return r.messages[f.ChatJID], nil
}

// ignoreFakeChatwoot is a minimal Chatwoot REST API: every contact lookup
// finds a contact, every contact has one open conversation, and each created
// message is recorded by its source_id. Reads it has no answer for find
// nothing; an unknown write fails the test.
type ignoreFakeChatwoot struct {
	mu        sync.Mutex
	contacts  []string
	sourceIDs []string
}

func (f *ignoreFakeChatwoot) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/contacts/search"):
			q := r.URL.Query().Get("q")
			f.mu.Lock()
			f.contacts = append(f.contacts, q)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{
				{"id": 7, "name": "x", "identifier": q, "phone_number": q},
			}})
		case strings.HasSuffix(r.URL.Path, "/inboxes"):
			// The inbox list, for code that looks up the inbox identifier.
			_, _ = w.Write([]byte(`{"payload":[{"id":2,"channel_type":"Channel::Api","inbox_identifier":"ignore-test-inbox"}]}`))
		case strings.HasSuffix(r.URL.Path, "/contacts/filter"):
			// The exact-match contact filter finds the contact too; the values
			// asked for count as lookups, like a search term.
			var filter struct {
				Payload []struct {
					AttributeKey string   `json:"attribute_key"`
					Values       []string `json:"values"`
				} `json:"payload"`
			}
			_ = json.NewDecoder(r.Body).Decode(&filter)
			contact := map[string]any{"id": 7, "name": "x"}
			f.mu.Lock()
			for _, condition := range filter.Payload {
				if len(condition.Values) == 0 {
					continue
				}
				f.contacts = append(f.contacts, condition.Values[0])
				if _, set := contact[condition.AttributeKey]; !set {
					contact[condition.AttributeKey] = condition.Values[0]
				}
			}
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"meta": map[string]any{"count": 1}, "payload": []map[string]any{contact}})
		case strings.HasSuffix(r.URL.Path, "/contacts/7"):
			_, _ = w.Write([]byte(`{}`)) // contact rename
		case strings.HasSuffix(r.URL.Path, "/contacts/7/conversations"):
			_, _ = w.Write([]byte(`{"payload":[{"id":42,"inbox_id":2,"status":"open"}]}`))
		case strings.HasSuffix(r.URL.Path, "/conversations/42/messages") && r.Method == http.MethodGet:
			// What the conversation already holds: nothing.
			_, _ = w.Write([]byte(`{"payload":[]}`))
		case strings.HasSuffix(r.URL.Path, "/conversations/42/messages"):
			body, _ := io.ReadAll(r.Body)
			var msg struct {
				SourceID string `json:"source_id"`
			}
			_ = json.Unmarshal(body, &msg)
			f.mu.Lock()
			f.sourceIDs = append(f.sourceIDs, msg.SourceID)
			id := 100 + len(f.sourceIDs)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case r.Method == http.MethodGet:
			// Any other read finds nothing. Only writes the fake does not know
			// are a failure: they would change what the test counts.
			_, _ = w.Write([]byte(`{"payload":[]}`))
		default:
			t.Errorf("unexpected Chatwoot request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *ignoreFakeChatwoot) uploaded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string(nil), f.sourceIDs...)
	sort.Strings(out)
	return out
}

func (f *ignoreFakeChatwoot) searched() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.contacts, ",")
}

func historyMsg(id, chat, sender string) *domainChatStorage.Message {
	return &domainChatStorage.Message{
		ID: id, ChatJID: chat, DeviceID: "628000@s.whatsapp.net", Sender: sender,
		Content: "text " + id, Timestamp: time.Now().Add(-time.Hour),
	}
}

// runHistoryIgnoreSync imports the repo's history into a fake Chatwoot and
// returns the WAID source ids that were actually uploaded.
func runHistoryIgnoreSync(t *testing.T, repo *historyIgnoreRepo, waClient *whatsmeow.Client, deviceIgnore []string) (*ignoreFakeChatwoot, *SyncProgress) {
	t.Helper()
	fake := &ignoreFakeChatwoot{}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)

	repo.chatwootSyncLinkRepo = newChatwootSyncLinkTestRepo()
	svc := NewSyncService(&Client{BaseURL: srv.URL, APIToken: "t", AccountID: 1, InboxID: 2, HTTPClient: srv.Client()}, repo)
	opts := DefaultSyncOptions()
	opts.IncludeMedia = false
	opts.DelayBetweenBatches = 0
	opts.IgnoreJids = deviceIgnore
	progress, err := svc.SyncHistory(context.Background(), "628000@s.whatsapp.net", waClient, opts)
	if err != nil {
		t.Fatalf("SyncHistory: %v", err)
	}
	return fake, progress
}

// TestSyncHistoryDoesNotUploadIgnoredChatsOrSenders runs a real REST history
// import against a fake Chatwoot and checks what reached it: an ignored chat is
// never touched, and inside a mirrored group the messages of an ignored
// participant are left out, exactly as live forwarding leaves them out.
func TestSyncHistoryDoesNotUploadIgnoredChatsOrSenders(t *testing.T) {
	prev := config.ChatwootIgnoreJids
	config.ChatwootIgnoreJids = []string{"628333@s.whatsapp.net"} // global: a participant
	t.Cleanup(func() { config.ChatwootIgnoreJids = prev })

	const (
		mirrored = "120363000000000001@g.us"
		ignored  = "120363000000000002@g.us"
		dm       = "628444@s.whatsapp.net"
	)
	newRepo := func() *historyIgnoreRepo {
		return &historyIgnoreRepo{
			chats: []*domainChatStorage.Chat{{JID: mirrored}, {JID: ignored}, {JID: dm}},
			messages: map[string][]*domainChatStorage.Message{
				mirrored: {
					historyMsg("g1-ana", mirrored, "628111@s.whatsapp.net"),
					historyMsg("g1-bob", mirrored, "628222@s.whatsapp.net"), // device-ignored sender
					historyMsg("g1-cat", mirrored, "628333@s.whatsapp.net"), // globally ignored sender
				},
				ignored: {historyMsg("g2-ana", ignored, "628111@s.whatsapp.net")},
				dm:      {historyMsg("dm-dan", dm, dm)},
			},
		}
	}

	// Without a device list only the global entry applies.
	fake, _ := runHistoryIgnoreSync(t, newRepo(), nil, nil)
	if got, want := strings.Join(fake.uploaded(), ","), "WAID:dm-dan,WAID:g1-ana,WAID:g1-bob,WAID:g2-ana"; got != want {
		t.Fatalf("global list only: uploaded %s, want %s", got, want)
	}

	fake, progress := runHistoryIgnoreSync(t, newRepo(), nil, []string{ignored, "628222@s.whatsapp.net"})
	if got, want := strings.Join(fake.uploaded(), ","), "WAID:dm-dan,WAID:g1-ana"; got != want {
		t.Fatalf("with the device list: uploaded %s, want %s", got, want)
	}
	if strings.Contains(fake.searched(), ignored) {
		t.Fatalf("an ignored chat must not even be looked up in Chatwoot; searched %s", fake.searched())
	}
	if progress.SyncedChats != 2 || progress.TotalMessages != 2 || progress.SyncedMessages != 2 || progress.FailedMessages != 0 {
		t.Fatalf("progress must count only what was mirrored: %+v", progress)
	}
}

// TestSyncHistoryMatchesLIDEntriesAgainstStoredPhoneJIDs covers ignore entries
// written as a LID: chat storage keeps the resolved phone JID, so the LID is
// recovered from the device's mapping table before matching (and the reverse
// for a chat stored under an unresolved-at-the-time LID).
func TestSyncHistoryMatchesLIDEntriesAgainstStoredPhoneJIDs(t *testing.T) {
	const (
		mappedDM   = "628111@s.whatsapp.net" // LID 123@lid
		otherDM    = "628444@s.whatsapp.net"
		storedLID  = "777@lid" // phone 628777@s.whatsapp.net
		storedLID2 = "888@lid" // no mapping
	)
	waClient := &whatsmeow.Client{Store: &store.Device{LIDs: &fakeLIDStore{pnToLID: map[string]string{
		mappedDM:                "123@lid",
		"628777@s.whatsapp.net": storedLID,
	}}}}
	newRepo := func() *historyIgnoreRepo {
		return &historyIgnoreRepo{
			chats: []*domainChatStorage.Chat{{JID: mappedDM}, {JID: otherDM}, {JID: storedLID}, {JID: storedLID2}},
			messages: map[string][]*domainChatStorage.Message{
				mappedDM:   {historyMsg("m1", mappedDM, mappedDM)},
				otherDM:    {historyMsg("m2", otherDM, otherDM)},
				storedLID:  {historyMsg("m3", storedLID, storedLID)},
				storedLID2: {historyMsg("m4", storedLID2, storedLID2)},
			},
		}
	}

	fake, _ := runHistoryIgnoreSync(t, newRepo(), waClient, []string{"123@lid", "628777@s.whatsapp.net"})
	if got, want := strings.Join(fake.uploaded(), ","), "WAID:m2,WAID:m4"; got != want {
		t.Fatalf("exact LID / phone entries: uploaded %s, want %s", got, want)
	}

	// Without the mapping table only the stored form can match.
	fake, _ = runHistoryIgnoreSync(t, newRepo(), nil, []string{"123@lid", "628777@s.whatsapp.net"})
	if got, want := strings.Join(fake.uploaded(), ","), "WAID:m1,WAID:m2,WAID:m3,WAID:m4"; got != want {
		t.Fatalf("no LID mappings: uploaded %s, want %s", got, want)
	}
}

func TestFilterIgnoredSendersResolvesLID(t *testing.T) {
	const group = "120363000000000001@g.us"
	r := newJIDFormResolver(&whatsmeow.Client{Store: &store.Device{LIDs: &fakeLIDStore{pnToLID: map[string]string{
		"628111@s.whatsapp.net": "123@lid",
	}}}})
	messages := []*domainChatStorage.Message{
		historyMsg("a", group, "628111@s.whatsapp.net"), // LID 123@lid
		historyMsg("b", group, "628222@s.whatsapp.net"),
		historyMsg("c", group, "999@lid"), // unresolved LID sender
	}

	ids := func(in []*domainChatStorage.Message) string {
		var out []string
		for _, m := range in {
			out = append(out, m.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(filterIgnoredSenders(context.Background(), messages, []string{"123@lid"}, r)); got != "b,c" {
		t.Fatalf("exact LID entry kept %q, want b,c", got)
	}
	if got := ids(filterIgnoredSenders(context.Background(), messages, []string{"@lid"}, r)); got != "b" {
		t.Fatalf("@lid wildcard kept %q, want b", got)
	}
	if got := ids(filterIgnoredSenders(context.Background(), messages, nil, r)); got != "a,b,c" {
		t.Fatalf("empty list kept %q, want everything", got)
	}
}
