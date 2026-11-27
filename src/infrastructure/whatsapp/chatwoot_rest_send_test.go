package whatsapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type restSendRetryRepo struct {
	deliveredChatwootReplyTestRepo
	queued chan *domainChatStorage.ChatwootForwardEvent
}

func (r *restSendRetryRepo) EnqueueChatwootForwardEvent(event *domainChatStorage.ChatwootForwardEvent) error {
	r.queued <- event
	return nil
}

func (r *restSendRetryRepo) nextEvent(t *testing.T) *domainChatStorage.ChatwootForwardEvent {
	t.Helper()
	select {
	case queued := <-r.queued:
		return queued
	case <-time.After(3 * time.Second):
		t.Fatal("REST send did not reach the device retry queue")
		return nil
	}
}

func TestChatwootRESTSendSurvivesRequestAndRetry(t *testing.T) {
	for _, autoDownload := range []bool{false, true} {
		t.Run(fmt.Sprintf("auto_download=%t", autoDownload), func(t *testing.T) {
			testChatwootRESTSendRetry(t, autoDownload)
		})
	}
}

func testChatwootRESTSendRetry(t *testing.T, autoDownload bool) {
	enabled, events, media, auto := config.ChatwootEnabled, config.ChatwootEvents, config.PathMedia, config.WhatsappAutoDownloadMedia
	resolve := getChatwootClientFn
	t.Cleanup(func() {
		config.ChatwootEnabled, config.ChatwootEvents, config.PathMedia, config.WhatsappAutoDownloadMedia = enabled, events, media, auto
		getChatwootClientFn = resolve
	})
	config.ChatwootEnabled, config.ChatwootEvents, config.PathMedia, config.WhatsappAutoDownloadMedia = true, []string{"message"}, t.TempDir(), autoDownload

	from := types.NewADJID("441111111111", 0, 2)
	to := types.NewJID("442222222222", types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &from}}
	repo := &restSendRetryRepo{queued: make(chan *domainChatStorage.ChatwootForwardEvent, 1)}
	device := NewDeviceInstance("support", client, repo)
	ctx, cancel := context.WithCancel(ContextWithDevice(t.Context(), device))
	ctx = WithChatwootRESTSend(ctx)
	content := []byte("%PDF-1.4\nexample")
	hash := sha256.Sum256(content)
	RememberChatwootUpload(ctx, hash[:], content)
	msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		FileName: proto.String("Customer Receipt.pdf"), Mimetype: proto.String("application/pdf"),
		Caption: proto.String("Your receipt"), FileSHA256: hash[:],
	}}
	if !autoDownload {
		for _, untouched := range []context.Context{t.Context(), WithChatwootRESTSend(t.Context())} {
			data := map[string]any{}
			require.NoError(t, buildMediaFields(untouched, nil, msg, data, "remote-only"))
			require.Empty(t, extractMediaPath(data["document"]))
			require.Equal(t, "Customer Receipt.pdf", data["document"].(map[string]any)["filename"])
		}
	}
	getChatwootClientFn = func(deviceID string) (*chatwoot.ResolvedConfig, error) {
		if deviceID != from.ToNonAD().String() {
			t.Errorf("unexpected Chatwoot device %q", deviceID)
		}
		return nil, errors.New("temporary lookup failure")
	}
	textCtx := WithChatwootRESTSend(ContextWithDevice(t.Context(), device))
	ForwardRESTSendToChatwoot(textCtx, client, to, &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("Hello")},
	}, whatsmeow.SendResponse{ID: "rest-text"})
	textEvent := repo.nextEvent(t)
	var textPayload WebhookEvent
	require.NoError(t, json.Unmarshal([]byte(textEvent.PayloadJSON), &textPayload))
	require.Equal(t, "message", textPayload.Event)
	require.Equal(t, "rest-text", textPayload.Payload["id"])
	require.Equal(t, true, textPayload.Payload["is_from_me"])
	require.Equal(t, "Hello", textPayload.Payload["body"])

	sent := whatsmeow.SendResponse{ID: "rest-file", Timestamp: time.Unix(1100, 0), Chat: types.NewJID("1234", types.HiddenUserServer)}
	ForwardRESTSendToChatwoot(ctx, client, to, msg, sent)
	cancel()
	device.ResetClient()

	queued := repo.nextEvent(t)
	require.Equal(t, from.ToNonAD().String(), queued.DeviceID)
	require.Equal(t, sent.ID, queued.WhatsAppMessageID)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(queued.PayloadJSON), &payload))
	data := payload["payload"].(map[string]any)
	require.Equal(t, to.String(), data["chat_id"])
	require.Equal(t, sent.Chat.String(), data["chat_lid"])
	path := extractMediaPath(data["document"])
	stored, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, stored)

	transport := chatwootRoutingTestTransport(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		switch r.URL.Path {
		case "/api/v1/accounts/1/contacts/search":
			require.Equal(t, "+"+to.User, r.URL.Query().Get("q"))
			io.WriteString(w, `{"payload":[{"id":7,"name":"442222222222","phone_number":"+442222222222"}]}`)
		case "/api/v1/accounts/1/contacts/7/conversations":
			io.WriteString(w, `{"payload":[{"id":42,"inbox_id":2,"status":"open"}]}`)
		case "/api/v1/accounts/1/conversations/42/messages":
			file, header, err := r.FormFile("attachments[]")
			require.NoError(t, err)
			defer file.Close()
			got, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, content, got)
			require.Equal(t, "Customer Receipt.pdf", header.Filename)
			require.Equal(t, "Your receipt", r.FormValue("content"))
			require.Equal(t, "outgoing", r.FormValue("message_type"))
			require.Equal(t, "WAID:rest-file", r.FormValue("source_id"))
			io.WriteString(w, `{"id":967}`)
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return w.Result(), nil
	})
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		return &chatwoot.ResolvedConfig{ConfigID: 3, Client: &chatwoot.Client{
			BaseURL: "https://chatwoot.example", APIToken: "test", AccountID: 1, InboxID: 2,
			HTTPClient: &http.Client{Transport: transport},
		}}, nil
	}
	oldManager := deviceManager
	t.Cleanup(func() { deviceManager = oldManager })
	deviceManager = NewDeviceManager(nil, nil, nil)
	deviceManager.AddDevice(NewDeviceInstance("support", nil, repo))
	require.Error(t, processChatwootForwardRetryEvent(repo, queued), "a logged-out slot keeps the message for its account")
	deviceManager.AddDevice(NewDeviceInstance("support", client, repo))
	require.NoError(t, processChatwootForwardRetryEvent(repo, queued))
	require.NotNil(t, repo.storedLink)
	require.Equal(t, "outgoing", repo.storedLink.Direction)
	require.Equal(t, int64(3), repo.storedLink.ChatwootConfigID)
	require.True(t, chatwoot.IsMessageSentByUs(1, 967))
	require.NoFileExists(t, path, "delivered mirror media is removed")

	// Chatwoot rejects the attachment: the caption still arrives, as text.
	require.NoError(t, os.WriteFile(path, content, 0600))
	data["id"] = "rest-large"
	rejected, err := json.Marshal(payload)
	require.NoError(t, err)
	transport = func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		switch r.URL.Path {
		case "/api/v1/accounts/1/contacts/search":
			io.WriteString(w, `{"payload":[{"id":7,"name":"442222222222","phone_number":"+442222222222"}]}`)
		case "/api/v1/accounts/1/contacts/7/conversations":
			io.WriteString(w, `{"payload":[{"id":42,"inbox_id":2,"status":"open"}]}`)
		case "/api/v1/accounts/1/conversations/42/messages":
			if r.Header.Get("Content-Type") != "application/json" {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				break
			}
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "Your receipt", body["content"])
			io.WriteString(w, `{"id":968}`)
		default:
			t.Fatalf("unexpected request %s", r.URL)
		}
		return w.Result(), nil
	}
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		return &chatwoot.ResolvedConfig{ConfigID: 3, Client: &chatwoot.Client{
			BaseURL: "https://chatwoot.example", APIToken: "test", AccountID: 1, InboxID: 2,
			HTTPClient: &http.Client{Transport: transport},
		}}, nil
	}
	queued.PayloadJSON, queued.WhatsAppMessageID = string(rejected), "rest-large"
	require.NoError(t, processChatwootForwardRetryEvent(repo, queued))
	require.True(t, chatwoot.IsMessageSentByUs(1, 968))
	require.NoFileExists(t, path)

	// A slot re-paired to another account can never deliver this message.
	require.NoError(t, os.WriteFile(path, content, 0600))
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		t.Error("a re-paired slot must not deliver another account's message")
		return nil, nil
	}
	deviceManager.AddDevice(companionInstance("support", "449999999999", 1))
	require.NoError(t, processChatwootForwardRetryEvent(repo, queued), "the job is dropped, not retried")
	require.NoFileExists(t, path)
}

func TestChatwootQueuedLocationKeepsCoordinates(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"location": &waE2E.LocationMessage{
		DegreesLatitude: proto.Float64(-6.2), DegreesLongitude: proto.Float64(106.8), Name: proto.String("HQ"),
	}})
	require.NoError(t, err)
	var queued map[string]any
	require.NoError(t, json.Unmarshal(raw, &queued))
	require.Equal(t, "Location: HQ (-6.200000, 106.800000)", extractStructuredMessageContent(queued))
}

func TestChatwootRESTSendOnlyOptsInToAllowedMessages(t *testing.T) {
	enabled, events, webhookEvents := config.ChatwootEnabled, config.ChatwootEvents, config.WhatsappWebhookEvents
	t.Cleanup(func() {
		config.ChatwootEnabled, config.ChatwootEvents, config.WhatsappWebhookEvents = enabled, events, webhookEvents
	})
	ctx := t.Context()
	config.ChatwootEnabled = false
	require.Same(t, ctx, WithChatwootRESTSend(ctx))
	config.ChatwootEnabled = true
	config.ChatwootEvents, config.WhatsappWebhookEvents = nil, []string{"group.joined"}
	require.Same(t, ctx, WithChatwootRESTSend(ctx))
	config.ChatwootEvents = []string{"message"}
	recipient := types.NewJID("442222222222", types.DefaultUserServer)
	restCtx := WithChatwootRESTSend(ctx)
	msg := &waE2E.Message{Conversation: proto.String("hello")}
	require.True(t, shouldMirrorRESTSend(restCtx, recipient, msg, "ack"))
	require.False(t, shouldMirrorRESTSend(ctx, recipient, msg, "ack"), "callbacks and workers use their own context")
	require.False(t, shouldMirrorRESTSend(restCtx, recipient, msg, ""))
	RememberChatwootUpload(restCtx, []byte("hash"), []byte("upload"))
	ForwardRESTSendToChatwoot(restCtx, nil, types.StatusBroadcastJID, msg, whatsmeow.SendResponse{ID: "ack"})
	require.Empty(t, restCtx.Value(restSendKey{}), "a skipped mirror still releases the request's upload bytes")
	interactive := &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{}}
	require.True(t, shouldMirrorRESTSend(restCtx, recipient, &waE2E.Message{
		ViewOnceMessage: &waE2E.FutureProofMessage{Message: interactive},
	}, "ack"), "ordinary interactive cards use the v1 envelope on the wire")
	private := &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}}
	for _, viewOnce := range []*waE2E.Message{
		{ViewOnceMessage: private},
		{ViewOnceMessageV2: private},
		{ViewOnceMessageV2Extension: private},
		{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ViewOnceMessage: private}}},
		{AudioMessage: &waE2E.AudioMessage{ViewOnce: proto.Bool(true)}},
		{ImageMessage: &waE2E.ImageMessage{ViewOnce: proto.Bool(true)}},
		{VideoMessage: &waE2E.VideoMessage{ViewOnce: proto.Bool(true)}},
	} {
		require.False(t, shouldMirrorRESTSend(restCtx, recipient, viewOnce, "ack"))
	}
	for _, excluded := range []types.JID{
		types.StatusBroadcastJID, types.NewJID("123", types.BroadcastServer),
		types.NewJID("123", types.NewsletterServer), types.NewJID("0", types.DefaultUserServer),
	} {
		require.False(t, shouldMirrorRESTSend(restCtx, excluded, msg, "ack"))
	}
}
