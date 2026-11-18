package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// Synthetic fixtures: slot-a sends as device-a to peer-a, in Chatwoot
// conversation 7 (account 3, inbox 5).
const (
	replyTestDevice = "device-a@s.whatsapp.net"
	replyTestChat   = "peer-a@s.whatsapp.net"
	replyTestLID    = "peer-a@lid"
	replyTestURL    = "https://files.example.test/a.bin"
)

// replySend is one send made by the webhook handler; reply is "" when the
// request carries no quote.
type replySend struct {
	method, phone, text, url, reply string
	ptt                             bool
	device                          *whatsapp.DeviceInstance
}

// replySendStub records the sends; fail maps a 1-based call number to its error.
type replySendStub struct {
	domainSend.ISendUsecase
	calls []replySend
	fail  map[int]error
}

func (s *replySendStub) record(ctx context.Context, call replySend, reply *string) (domainSend.GenericResponse, error) {
	if reply != nil {
		call.reply = *reply
	}
	call.device, _ = whatsapp.DeviceFromContext(ctx)
	s.calls = append(s.calls, call)
	if err := s.fail[len(s.calls)]; err != nil {
		return domainSend.GenericResponse{}, err
	}
	return domainSend.GenericResponse{MessageID: fmt.Sprintf("WA-SENT-%d", len(s.calls))}, nil
}

func (s *replySendStub) SendText(ctx context.Context, r domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	return s.record(ctx, replySend{method: "SendText", phone: r.Phone, text: r.Message}, r.ReplyMessageID)
}

func (s *replySendStub) SendImage(ctx context.Context, r domainSend.ImageRequest) (domainSend.GenericResponse, error) {
	return s.record(ctx, replySend{method: "SendImage", phone: r.Phone, text: r.Caption, url: *r.ImageURL}, r.ReplyMessageID)
}

func (s *replySendStub) SendAudio(ctx context.Context, r domainSend.AudioRequest) (domainSend.GenericResponse, error) {
	return s.record(ctx, replySend{method: "SendAudio", phone: r.Phone, url: *r.AudioURL, ptt: r.PTT}, r.ReplyMessageID)
}

func (s *replySendStub) SendVideo(ctx context.Context, r domainSend.VideoRequest) (domainSend.GenericResponse, error) {
	return s.record(ctx, replySend{method: "SendVideo", phone: r.Phone, text: r.Caption, url: *r.VideoURL}, r.ReplyMessageID)
}

func (s *replySendStub) SendFile(ctx context.Context, r domainSend.FileRequest) (domainSend.GenericResponse, error) {
	return s.record(ctx, replySend{method: "SendFile", phone: r.Phone, text: r.Caption, url: *r.FileURL}, r.ReplyMessageID)
}

// replyRepo adds the reads and writes of the reply path to the route stub.
// A stored message is returned even with messageErr, so the handler must
// check the error itself.
type replyRepo struct {
	chatwootRouteTestRepo
	messages   map[string]domainChatStorage.Message // by device + " " + id
	messageErr error
	reads      []string
	links      []domainChatStorage.ChatwootMessageLink
	writes     int
	writeErr   error
	lookups    []domainChatStorage.ChatwootReplyLinkFilter
	lookupErr  error
	counts     int
	configs    []*domainChatStorage.ChatwootDeviceConfig
}

// CountChatwootDeviceConfigs counts the legacy-mode checks: routing makes two,
// and a numeric quote one more.
func (r *replyRepo) CountChatwootDeviceConfigs() (int, error) {
	r.counts++
	return r.chatwootRouteTestRepo.CountChatwootDeviceConfigs()
}

func (r *replyRepo) ListChatwootDeviceConfigs() ([]*domainChatStorage.ChatwootDeviceConfig, error) {
	return r.configs, nil
}

func (r *replyRepo) GetChatwootDeviceConfigByIdentifier(identifier string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	for _, cfg := range r.configs {
		if cfg.DeviceID == identifier || cfg.DeviceJID == identifier {
			return cfg, nil
		}
	}
	return nil, nil
}

func (r *replyRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.reads = append(r.reads, deviceID+" "+id)
	if message, ok := r.messages[deviceID+" "+id]; ok {
		return &message, r.messageErr
	}
	return nil, r.messageErr
}

// GetChatwootMessageLinkForReply records the filter and returns the first
// stored link of that device and Chatwoot message; the scope is asserted on
// the recorded filter. A found link is returned even with lookupErr.
func (r *replyRepo) GetChatwootMessageLinkForReply(filter domainChatStorage.ChatwootReplyLinkFilter) (*domainChatStorage.ChatwootMessageLink, error) {
	r.lookups = append(r.lookups, filter)
	for _, link := range r.links {
		if link.DeviceID == filter.DeviceID && link.ChatwootMessageID == filter.ChatwootMessageID {
			return &link, r.lookupErr
		}
	}
	return nil, r.lookupErr
}

func (r *replyRepo) UpsertChatwootMessageLink(link *domainChatStorage.ChatwootMessageLink) error {
	r.writes++
	if r.writeErr != nil {
		return r.writeErr
	}
	r.links = append(r.links, *link)
	return nil
}

// replyLIDs maps peer-a's LID to its phone JID while mapped is set.
type replyLIDs struct {
	store.LIDStore
	mapped bool
}

func (s *replyLIDs) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if s.mapped && lid.User == "peer-a" {
		return types.NewJID("peer-a", types.DefaultUserServer), nil
	}
	return types.EmptyJID, nil
}

type replyEnv struct {
	repo   *replyRepo
	sender *replySendStub
	lids   *replyLIDs
	device *whatsapp.DeviceInstance
	app    *fiber.App
	notes  atomic.Int32
}

// newReplyEnv routes conversation 7 through its link to slot-a and peer-a, in
// legacy mode, so failure notes go through the env Chatwoot client to a server
// that counts them.
func newReplyEnv(t *testing.T) *replyEnv {
	t.Helper()
	withChatwootWebhookSecret(t, "reply-test-secret")
	chatwootReplays.reset()
	t.Cleanup(chatwootReplays.reset)
	env := &replyEnv{
		repo: &replyRepo{chatwootRouteTestRepo: chatwootRouteTestRepo{link: &domainChatStorage.ChatwootMessageLink{
			DeviceID: replyTestDevice, WhatsAppChatJID: replyTestChat,
			ChatwootConversationID: 7, ChatwootAccountID: 3,
		}}},
		sender: &replySendStub{},
		lids:   &replyLIDs{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		env.notes.Add(1)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(server.Close)

	prevURL, prevToken := config.ChatwootURL, config.ChatwootAPIToken
	prevAccount, prevInbox := config.ChatwootAccountID, config.ChatwootInboxID
	t.Cleanup(func() {
		config.ChatwootURL, config.ChatwootAPIToken = prevURL, prevToken
		config.ChatwootAccountID, config.ChatwootInboxID = prevAccount, prevInbox
	})
	config.ChatwootURL, config.ChatwootAPIToken = server.URL, "token"
	config.ChatwootAccountID, config.ChatwootInboxID = 3, 5
	chatwoot.InitClientRegistry(nil)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	jid := types.NewJID("device-a", types.DefaultUserServer)
	env.device = whatsapp.NewDeviceInstance("slot-a", &whatsmeow.Client{Store: &store.Device{ID: &jid, LIDs: env.lids}}, nil)
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	dm.AddDevice(env.device)

	handler := &ChatwootHandler{SendUsecase: env.sender, DeviceManager: dm, ChatStorageRepo: env.repo}
	env.app = fiber.New()
	env.app.Post("/chatwoot/webhook", handler.HandleWebhook)
	return env
}

func (e *replyEnv) store(deviceID, id, chatJID string) {
	if e.repo.messages == nil {
		e.repo.messages = map[string]domainChatStorage.Message{}
	}
	e.repo.messages[deviceID+" "+id] = domainChatStorage.Message{ID: id, ChatJID: chatJID, DeviceID: deviceID}
}

// seed stores the outbound link of an earlier agent message and, as the send
// usecase does after delivering it, its WhatsApp message in the same chat.
func (e *replyEnv) seed(chatwootMessageID int, waMessageID, chatJID string) {
	e.repo.links = append(e.repo.links, domainChatStorage.ChatwootMessageLink{
		DeviceID: replyTestDevice, WhatsAppMessageID: waMessageID, WhatsAppChatJID: chatJID, ChatwootMessageID: chatwootMessageID,
	})
	e.store(replyTestDevice, waMessageID, chatJID)
}

func (e *replyEnv) post(t *testing.T, body string) {
	t.Helper()
	timestamp := nowUnix(0)
	status := postChatwootWebhook(t, e.app, webhookAuthRequest{
		body: body,
		headers: map[string]string{
			"X-Chatwoot-Timestamp": timestamp,
			"X-Chatwoot-Signature": chatwootSign("reply-test-secret", timestamp, body),
		},
	})
	if status != fiber.StatusOK {
		t.Fatalf("webhook status = %d, want 200", status)
	}
}

// send is the expected record of a send from slot-a.
func (e *replyEnv) send(method, phone, text, url, reply string) replySend {
	return replySend{method: method, phone: phone, text: text, url: url, reply: reply, ptt: method == "SendAudio", device: e.device}
}

func (e *replyEnv) assertSends(t *testing.T, want ...replySend) {
	t.Helper()
	if !reflect.DeepEqual(e.sender.calls, want) {
		t.Fatalf("sends = %+v, want %+v", e.sender.calls, want)
	}
}

// replyBody is agent message 90 in conversation 7, as the JSON Chatwoot sends,
// so numbers decode as float64 like in production. fields override the top
// level.
func replyBody(t *testing.T, attrs map[string]any, fields map[string]any) string {
	t.Helper()
	body := map[string]any{
		"event": "message_created", "message_type": "outgoing", "id": 90, "content": "Olá",
		"account":            map[string]any{"id": 3},
		"conversation":       map[string]any{"id": 7, "inbox_id": 5},
		"content_attributes": attrs,
	}
	for key, value := range fields {
		body[key] = value
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}
	return string(raw)
}

// quoting is a reply to the mirrored WhatsApp message id; Chatwoot also sends
// the quoted message's own numeric id, 41.
func quoting(id string) map[string]any {
	return map[string]any{"in_reply_to": 41, "in_reply_to_external_id": "WAID:" + id}
}

// quotingID is a reply to an earlier agent message, which has no source_id.
func quotingID(id any) map[string]any {
	return map[string]any{"in_reply_to": id, "in_reply_to_external_id": nil}
}

func attachments(fileTypes ...string) map[string]any {
	list := make([]map[string]any, len(fileTypes))
	for i, fileType := range fileTypes {
		list[i] = map[string]any{"id": i + 1, "file_type": fileType, "data_url": replyTestURL}
	}
	return map[string]any{"attachments": list, "content": "Legenda"}
}

// chatwootSameChat compares full non-AD JIDs and maps an @lid side only
// through the device in the context.
func TestChatwootSameChat(t *testing.T) {
	jid := types.NewJID("device-a", types.DefaultUserServer)
	lids := &replyLIDs{}
	ctx := whatsapp.ContextWithDevice(context.Background(),
		whatsapp.NewDeviceInstance("slot-a", &whatsmeow.Client{Store: &store.Device{ID: &jid, LIDs: lids}}, nil))

	cases := []struct {
		name   string
		a, b   string
		mapped bool
		want   bool
	}{
		{"same phone JID", replyTestChat, replyTestChat, false, true},
		{"device suffix ignored", "peer-a:12@s.whatsapp.net", replyTestChat, false, true},
		{"other user", "peer-b@s.whatsapp.net", replyTestChat, true, false},
		{"same user on another server", "peer-a@g.us", replyTestChat, true, false},
		{"LID mapped to the phone JID", replyTestLID, replyTestChat, true, true},
		{"phone JID mapped to the LID", replyTestChat, replyTestLID, true, true},
		{"LID without a mapping", replyTestLID, replyTestChat, false, false},
		{"same LID without a mapping", replyTestLID, replyTestLID, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lids.mapped = tc.mapped
			if got := chatwootSameChat(ctx, tc.a, tc.b); got != tc.want {
				t.Fatalf("chatwootSameChat(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// A WAID reference is quoted only when that message is stored for the sending
// device (read by its storage JID) in the reply's chat. Otherwise the reply
// goes out unquoted, with no failure note.
func TestChatwootReplyQuotesAStoredMessageOfTheSameChat(t *testing.T) {
	cases := []struct {
		name        string
		destination string
		setup       func(*replyEnv)
		wantPhone   string
		wantReply   string
	}{
		{"same chat", replyTestChat, func(e *replyEnv) { e.store(replyTestDevice, "WA-ORIG", replyTestChat) }, "peer-a", "WA-ORIG"},
		{"group", "group-a@g.us", func(e *replyEnv) { e.store(replyTestDevice, "WA-ORIG", "group-a@g.us") }, "group-a@g.us", "WA-ORIG"},
		{"stored under the LID the device maps", replyTestChat, func(e *replyEnv) {
			e.lids.mapped = true
			e.store(replyTestDevice, "WA-ORIG", replyTestLID)
		}, "peer-a", "WA-ORIG"},
		{"LID destination the device maps", replyTestLID, func(e *replyEnv) {
			e.lids.mapped = true
			e.store(replyTestDevice, "WA-ORIG", replyTestChat)
		}, replyTestLID, "WA-ORIG"},
		{"stored under an unmapped LID", replyTestChat, func(e *replyEnv) { e.store(replyTestDevice, "WA-ORIG", replyTestLID) }, "peer-a", ""},
		{"another chat", replyTestChat, func(e *replyEnv) { e.store(replyTestDevice, "WA-ORIG", "peer-b@s.whatsapp.net") }, "peer-a", ""},
		{"stored for another device", replyTestChat, func(e *replyEnv) { e.store("device-b@s.whatsapp.net", "WA-ORIG", replyTestChat) }, "peer-a", ""},
		{"not stored", replyTestChat, func(*replyEnv) {}, "peer-a", ""},
		{"read error", replyTestChat, func(e *replyEnv) {
			e.store(replyTestDevice, "WA-ORIG", replyTestChat)
			e.repo.messageErr = errors.New("database is locked")
		}, "peer-a", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newReplyEnv(t)
			env.repo.link.WhatsAppChatJID = tc.destination
			tc.setup(env)

			env.post(t, replyBody(t, quoting("WA-ORIG"), nil))

			env.assertSends(t, env.send("SendText", tc.wantPhone, "Olá", "", tc.wantReply))
			if want := []string{replyTestDevice + " WA-ORIG"}; !reflect.DeepEqual(env.repo.reads, want) {
				t.Fatalf("message reads = %q, want %q", env.repo.reads, want)
			}
			if n := env.notes.Load(); n != 0 {
				t.Fatalf("failure notes = %d, want 0", n)
			}
		})
	}
}

// Without a usable WAID reference nothing is read and the reply goes out
// unquoted, even though WA-ORIG is stored in the chat.
func TestChatwootReplyWithoutAWAIDReference(t *testing.T) {
	cases := map[string]map[string]any{
		"no content_attributes":          nil,
		"null external id":               {"in_reply_to": nil, "in_reply_to_external_id": nil},
		"external id without the prefix": {"in_reply_to_external_id": "WA-ORIG"},
		"lowercase prefix":               {"in_reply_to_external_id": "waid:WA-ORIG"},
		"external id is not a string":    {"in_reply_to_external_id": 41},
	}
	for name, attrs := range cases {
		t.Run(name, func(t *testing.T) {
			env := newReplyEnv(t)
			env.store(replyTestDevice, "WA-ORIG", replyTestChat)

			env.post(t, replyBody(t, attrs, nil))

			env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", ""))
			if len(env.repo.reads) != 0 {
				t.Fatalf("message reads = %q, want none", env.repo.reads)
			}
		})
	}
}

// Every attachment type reaches its send method with the quote, the
// destination and the URL.
func TestChatwootReplyQuotesEveryAttachmentType(t *testing.T) {
	cases := []struct{ fileType, method, caption string }{
		{"image", "SendImage", "Legenda"},
		{"audio", "SendAudio", ""},
		{"video", "SendVideo", "Legenda"},
		{"file", "SendFile", "Legenda"},
		{"location", "SendFile", "Legenda"},
	}
	for _, tc := range cases {
		t.Run(tc.fileType, func(t *testing.T) {
			env := newReplyEnv(t)
			env.store(replyTestDevice, "WA-ORIG", replyTestChat)

			env.post(t, replyBody(t, quoting("WA-ORIG"), attachments(tc.fileType)))

			env.assertSends(t, env.send(tc.method, "peer-a", tc.caption, replyTestURL, "WA-ORIG"))
		})
	}
}

// Each file of a reply quotes the original; the caption stays on the first.
func TestChatwootReplyQuotesEveryFile(t *testing.T) {
	env := newReplyEnv(t)
	env.store(replyTestDevice, "WA-ORIG", replyTestChat)

	env.post(t, replyBody(t, quoting("WA-ORIG"), attachments("image", "video", "file")))

	env.assertSends(t,
		env.send("SendImage", "peer-a", "Legenda", replyTestURL, "WA-ORIG"),
		env.send("SendVideo", "peer-a", "", replyTestURL, "WA-ORIG"),
		env.send("SendFile", "peer-a", "", replyTestURL, "WA-ORIG"),
	)
}

// The audio-to-file fallback keeps the quote, the URL and the caption, and a
// fallback that succeeds posts no failure note.
func TestChatwootReplyAudioFallbackKeepsTheQuote(t *testing.T) {
	env := newReplyEnv(t)
	env.store(replyTestDevice, "WA-ORIG", replyTestChat)
	env.sender.fail = map[int]error{1: errors.New("unsupported audio")}

	env.post(t, replyBody(t, quoting("WA-ORIG"), attachments("audio")))

	env.assertSends(t,
		env.send("SendAudio", "peer-a", "", replyTestURL, "WA-ORIG"),
		env.send("SendFile", "peer-a", "Legenda", replyTestURL, "WA-ORIG"),
	)
	if n := env.notes.Load(); n != 0 {
		t.Fatalf("failure notes = %d, want 0", n)
	}
}

// When the first file fails, the next one still quotes the original, nothing
// is resent without the quote, and only the failed file gets a note.
func TestChatwootReplyPartialAttachmentFailureKeepsTheQuote(t *testing.T) {
	env := newReplyEnv(t)
	env.store(replyTestDevice, "WA-ORIG", replyTestChat)
	env.sender.fail = map[int]error{1: errors.New("upload failed")}

	env.post(t, replyBody(t, quoting("WA-ORIG"), attachments("image", "video")))

	env.assertSends(t,
		env.send("SendImage", "peer-a", "Legenda", replyTestURL, "WA-ORIG"),
		env.send("SendVideo", "peer-a", "", replyTestURL, "WA-ORIG"),
	)
	if n := env.notes.Load(); n != 1 {
		t.Fatalf("failure notes = %d, want 1", n)
	}
}

// Private notes, incoming messages and echoes of mirrored messages stop
// before the quote is resolved, so nothing is read or sent; the same reply
// without a source_id is sent quoted.
func TestChatwootReplyGuardsRunBeforeTheQuote(t *testing.T) {
	refs := []struct {
		name  string
		attrs map[string]any
		quote string
	}{
		{"WAID", quoting("WA-ORIG"), "WA-ORIG"},
		{"numeric only", quotingID(41), "WA-AGENT"},
	}
	cases := []struct {
		name   string
		fields map[string]any
		sent   bool
	}{
		{"private note", map[string]any{"private": true}, false},
		{"incoming message", map[string]any{"message_type": "incoming"}, false},
		{"echo of a mirrored message", map[string]any{"source_id": "WAID:WA-ECHO"}, false},
		{"agent reply without source_id", map[string]any{"source_id": ""}, true},
	}
	for _, ref := range refs {
		for _, tc := range cases {
			t.Run(ref.name+"/"+tc.name, func(t *testing.T) {
				env := newReplyEnv(t)
				env.store(replyTestDevice, "WA-ORIG", replyTestChat)
				env.seed(41, "WA-AGENT", replyTestChat)

				env.post(t, replyBody(t, ref.attrs, tc.fields))

				if tc.sent {
					env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", ref.quote))
					return
				}
				env.assertSends(t)
				if reads := len(env.repo.reads) + len(env.repo.lookups); reads != 0 {
					t.Fatalf("quote reads = %d, want 0", reads)
				}
			})
		}
	}
}

// A link write that fails after a quoted send is not a delivery failure:
// one send, no resend, no failure note.
func TestChatwootReplyLinkWriteFailureDoesNotResend(t *testing.T) {
	env := newReplyEnv(t)
	env.store(replyTestDevice, "WA-ORIG", replyTestChat)
	env.repo.writeErr = errors.New("disk full")

	env.post(t, replyBody(t, quoting("WA-ORIG"), nil))

	env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", "WA-ORIG"))
	if env.repo.writes != 1 {
		t.Fatalf("link writes = %d, want 1", env.repo.writes)
	}
	if n := env.notes.Load(); n != 0 {
		t.Fatalf("failure notes = %d, want 0", n)
	}
}

// A reply to an earlier agent message quotes the WhatsApp message stored by
// that message's own delivery, looked up in exactly the scope its link was
// written with, in each routing mode.
func TestChatwootReplyQuotesAnEarlierAgentReply(t *testing.T) {
	modes := []struct {
		name     string
		configs  int
		configID int64
	}{
		{"per-device", 1, 11},
		{"legacy", 0, 0},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			env := newReplyEnv(t)
			env.repo.count = mode.configs
			env.repo.link.ChatwootConfigID = mode.configID
			if mode.configID != 0 {
				configs := &routeAuthRepo{configs: []*domainChatStorage.ChatwootDeviceConfig{{
					ID: mode.configID, DeviceID: "slot-a", DeviceJID: replyTestDevice,
					ChatwootURL: config.ChatwootURL, APIToken: "token", AccountID: 3, InboxID: 5, Enabled: true,
				}}}
				env.repo.configs = configs.configs
				chatwoot.InitClientRegistry(configs)
			}

			env.post(t, replyBody(t, nil, map[string]any{"id": 50, "content": "Primeira"}))
			env.store(replyTestDevice, "WA-SENT-1", replyTestChat) // stored by the send usecase
			env.post(t, replyBody(t, quotingID(50), map[string]any{"id": 51, "content": "Segunda"}))

			env.assertSends(t,
				env.send("SendText", "peer-a", "Primeira", "", ""),
				env.send("SendText", "peer-a", "Segunda", "", "WA-SENT-1"),
			)
			first := env.repo.links[0]
			if first.DeviceID != replyTestDevice || first.WhatsAppMessageID != "WA-SENT-1" || first.ChatwootMessageID != 50 ||
				first.ChatwootAccountID != 3 || first.ChatwootInboxID != 5 || first.ChatwootConfigID != mode.configID {
				t.Fatalf("first link = %+v, want device-a, WA-SENT-1, message 50, account 3, inbox 5, config %d", first, mode.configID)
			}
			want := domainChatStorage.ChatwootReplyLinkFilter{
				DeviceID: first.DeviceID, ChatwootMessageID: 50, ChatwootConversationID: first.ChatwootConversationID,
				ChatwootAccountID: first.ChatwootAccountID, ChatwootInboxID: first.ChatwootInboxID,
				ChatwootConfigID: first.ChatwootConfigID, AllowLegacyAccountZero: mode.configs == 0,
			}
			if !reflect.DeepEqual(env.repo.lookups, []domainChatStorage.ChatwootReplyLinkFilter{want}) {
				t.Fatalf("lookups = %+v, want [%+v]", env.repo.lookups, want)
			}
		})
	}
}

// The link must be in the reply's chat; an @lid chat counts only through the
// sending device's mapping. Otherwise the reply goes out unquoted, with no
// failure note. The stored message is in the reply's chat in every case.
func TestChatwootReplyChecksTheChatOfTheLink(t *testing.T) {
	cases := []struct {
		name, chat string
		mapped     bool
		want       string
	}{
		{"same chat", replyTestChat, false, "WA-AGENT"},
		{"another chat", "peer-b@s.whatsapp.net", true, ""},
		{"LID the device maps", replyTestLID, true, "WA-AGENT"},
		{"unmapped LID", replyTestLID, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newReplyEnv(t)
			env.lids.mapped = tc.mapped
			env.seed(41, "WA-AGENT", tc.chat)
			env.store(replyTestDevice, "WA-AGENT", replyTestChat)

			env.post(t, replyBody(t, quotingID(41), nil))

			env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", tc.want))
			if len(env.repo.lookups) != 1 {
				t.Fatalf("lookups = %d, want 1", len(env.repo.lookups))
			}
			if n := env.notes.Load(); n != 0 {
				t.Fatalf("failure notes = %d, want 0", n)
			}
		})
	}
}

// The quote is built from the message stored under the link's WhatsApp id,
// read by id and device only, so that row must be in the reply's chat too: a
// missing row, or one of another chat with the same id, sends no quote.
func TestChatwootReplyChecksTheChatOfTheStoredMessage(t *testing.T) {
	cases := []struct {
		name, stored, want string // stored is the chat of WA-AGENT's row, "" when missing
	}{
		{"stored in the reply's chat", replyTestChat, "WA-AGENT"},
		{"stored only in another chat", "peer-b@s.whatsapp.net", ""},
		{"not stored", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newReplyEnv(t)
			env.lids.mapped = true
			env.seed(41, "WA-AGENT", replyTestChat)
			delete(env.repo.messages, replyTestDevice+" WA-AGENT")
			if tc.stored != "" {
				env.store(replyTestDevice, "WA-AGENT", tc.stored)
			}

			env.post(t, replyBody(t, quotingID(41), nil))

			env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", tc.want))
			if want := []string{replyTestDevice + " WA-AGENT"}; !reflect.DeepEqual(env.repo.reads, want) {
				t.Fatalf("reads = %v, want %v", env.repo.reads, want)
			}
			if len(env.repo.lookups) != 1 {
				t.Fatalf("lookups = %d, want 1", len(env.repo.lookups))
			}
			if n := env.notes.Load(); n != 0 {
				t.Fatalf("failure notes = %d, want 0", n)
			}
		})
	}
}

// Without a link, or when the lookup fails, the reply goes out once,
// unquoted, with no failure note.
func TestChatwootReplyWithoutALinkIsSentUnquoted(t *testing.T) {
	for name, lookupErr := range map[string]error{"no link": nil, "lookup error": errors.New("database is locked")} {
		t.Run(name, func(t *testing.T) {
			env := newReplyEnv(t)
			if lookupErr != nil {
				env.seed(41, "WA-AGENT", replyTestChat)
				env.repo.lookupErr = lookupErr
			}

			env.post(t, replyBody(t, quotingID(41), nil))

			env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", ""))
			if len(env.repo.lookups) != 1 {
				t.Fatalf("lookups = %d, want 1", len(env.repo.lookups))
			}
			if n := env.notes.Load(); n != 0 {
				t.Fatalf("failure notes = %d, want 0", n)
			}
		})
	}
}

// in_reply_to is used only as an exact positive integer; any other value is
// sent unquoted, with no lookup and no extra legacy-mode check.
func TestChatwootReplyIgnoresAnInvalidInReplyTo(t *testing.T) {
	cases := []struct {
		name    string
		value   any
		want    string
		lookups int
	}{
		{"integer", 41, "WA-AGENT", 1},
		{"numeric string", "41", "", 0},
		{"fraction", 41.5, "", 0},
		{"zero", 0, "", 0},
		{"negative", -41, "", 0},
		{"beyond exact float64 integers", float64(1 << 53), "", 0},
		{"boolean", true, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newReplyEnv(t)
			env.seed(41, "WA-AGENT", replyTestChat)

			env.post(t, replyBody(t, quotingID(tc.value), nil))

			env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", tc.want))
			if len(env.repo.lookups) != tc.lookups || env.repo.counts != 2+tc.lookups {
				t.Fatalf("lookups = %+v, legacy-mode checks = %d; want %d and %d", env.repo.lookups, env.repo.counts, tc.lookups, 2+tc.lookups)
			}
		})
	}
}

// A WAID reference wins over in_reply_to: accepted or refused, the link of
// in_reply_to is never looked up and the legacy mode is not checked again.
func TestChatwootReplyPrefersTheWAIDReference(t *testing.T) {
	cases := []struct {
		name, stored, want string // stored is the chat of WA-ORIG, "" when not stored
	}{
		{"WAID accepted", replyTestChat, "WA-ORIG"},
		{"WAID of another chat", "peer-b@s.whatsapp.net", ""},
		{"WAID not stored", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newReplyEnv(t)
			env.seed(41, "WA-AGENT", replyTestChat)
			if tc.stored != "" {
				env.store(replyTestDevice, "WA-ORIG", tc.stored)
			}

			env.post(t, replyBody(t, quoting("WA-ORIG"), nil))

			env.assertSends(t, env.send("SendText", "peer-a", "Olá", "", tc.want))
			if len(env.repo.lookups) != 0 || env.repo.counts != 2 {
				t.Fatalf("lookups = %+v, legacy-mode checks = %d; want none and 2 (routing)", env.repo.lookups, env.repo.counts)
			}
		})
	}
}

// The quote of an earlier agent reply is resolved once and goes on every
// file, including the audio-to-file fallback.
func TestChatwootReplyQuotesAnEarlierAgentReplyOnEveryFile(t *testing.T) {
	env := newReplyEnv(t)
	env.seed(41, "WA-AGENT", replyTestChat)
	env.sender.fail = map[int]error{2: errors.New("unsupported audio")}

	env.post(t, replyBody(t, quotingID(41), attachments("image", "audio")))

	env.assertSends(t,
		env.send("SendImage", "peer-a", "Legenda", replyTestURL, "WA-AGENT"),
		env.send("SendAudio", "peer-a", "", replyTestURL, "WA-AGENT"),
		env.send("SendFile", "peer-a", "", replyTestURL, "WA-AGENT"),
	)
	if len(env.repo.lookups) != 1 {
		t.Fatalf("lookups = %d, want 1", len(env.repo.lookups))
	}
}

func TestChatwootReplyRejectsUnscopedLinkInPerDeviceMode(t *testing.T) {
	env := newReplyEnv(t)
	env.repo.count = 1
	env.seed(50, "WA-AGENT", replyTestChat)
	env.post(t, replyBody(t, quotingID(50), nil))
	if len(env.sender.calls) != 0 || len(env.repo.lookups) != 0 {
		t.Fatalf("unscoped link triggered sends=%+v lookups=%+v", env.sender.calls, env.repo.lookups)
	}
}
