package usecase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

type restMirrorRepo struct {
	domainChatStorage.IChatStorageRepository
}

func (r *restMirrorRepo) GetMessageByIDAndDevice(_, id string) (*domainChatStorage.Message, error) {
	return &domainChatStorage.Message{ID: id, Content: "Forward this"}, nil
}

func (r *restMirrorRepo) StoreSentMessageWithContext(context.Context, string, string, string, string, time.Time, *waE2E.Message) error {
	return nil
}

func TestChatwootMirrorFromRESTEndpoints(t *testing.T) {
	enabled, events := config.ChatwootEnabled, config.ChatwootEvents
	auto, limit, media := config.WhatsappAutoDownloadMedia, config.WhatsappSettingMaxDownloadSize, config.PathMedia
	registry := chatwoot.GetClientRegistry()
	t.Cleanup(func() {
		config.ChatwootEnabled, config.ChatwootEvents = enabled, events
		config.WhatsappAutoDownloadMedia, config.WhatsappSettingMaxDownloadSize, config.PathMedia = auto, limit, media
		chatwoot.SetClientRegistry(registry)
	})
	config.ChatwootEnabled, config.ChatwootEvents = true, []string{"message"}
	config.WhatsappAutoDownloadMedia, config.WhatsappSettingMaxDownloadSize, config.PathMedia = false, 1, t.TempDir()

	dbPath := filepath.Join(t.TempDir(), "chatstorage.db")
	openDB := func() (*sql.DB, domainChatStorage.IChatStorageRepository) {
		t.Helper()
		db, err := sql.Open(sqlite.DriverName, dbPath)
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })
		return db, chatstorage.NewStorageRepository(db)
	}
	db, storage := openDB()
	require.NoError(t, storage.InitializeSchema())
	repo := &restMirrorRepo{storage}
	chatwoot.InitClientRegistry(repo)
	sender := types.NewJID("441111111111", types.DefaultUserServer)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected Chatwoot request before the queue worker started: %s", r.URL.Path)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(destination.Close)
	require.NoError(t, repo.SaveChatwootDeviceConfig(&domainChatStorage.ChatwootDeviceConfig{
		DeviceID: "support", DeviceJID: sender.String(), ChatwootURL: destination.URL, APIToken: "test",
		AccountID: 1, InboxID: 2, Enabled: true,
	}))
	client := &whatsmeow.Client{Store: &store.Device{ID: &sender}}
	instance := whatsapp.NewDeviceInstance("support", client, repo)
	service := serviceSend{
		chatStorageRepo: repo,
		validateJIDFn:   func(_ *whatsmeow.Client, jid string) (types.JID, error) { return utils.ParseJID(jid) },
		uploadMediaFn: func(_ context.Context, _ *whatsmeow.Client, _ whatsmeow.MediaType, data []byte, _ types.JID) (whatsmeow.UploadResponse, error) {
			hash := sha256.Sum256(data)
			return whatsmeow.UploadResponse{FileSHA256: hash[:], FileLength: uint64(len(data))}, nil
		},
		sendMessageFn: func(_ *whatsmeow.Client, _ context.Context, to types.JID, msg *waE2E.Message, _ ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
			id := "forward-ack"
			if msg.GetDocumentMessage() != nil {
				id = "file-ack"
			}
			return whatsmeow.SendResponse{ID: id, Chat: to, Timestamp: time.Now()}, nil
		},
	}

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("device", instance)
		return c.Next()
	})
	rest.InitRestSend(app, service)
	rest.InitRestMessage(app, nil, service)
	send := func(req *http.Request) {
		t.Helper()
		resp, err := app.Test(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	const content = "Attachment larger than the incoming download limit"
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	require.NoError(t, form.WriteField("phone", "442222222222"))
	require.NoError(t, form.WriteField("caption", "Your receipt"))
	file, err := form.CreateFormFile("file", "Receipt Copy.txt")
	require.NoError(t, err)
	_, err = file.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, form.Close())
	req := httptest.NewRequest(http.MethodPost, "/send/file", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	send(req)
	req = httptest.NewRequest(http.MethodPost, "/message/original/forward", strings.NewReader(`{"phone":"442222222222"}`))
	req.Header.Set("Content-Type", "application/json")
	send(req)

	// Both HTTP sends have returned; the mirrors are queued in the background.
	require.Eventually(t, func() bool {
		var queued int
		return db.QueryRow(`SELECT COUNT(*) FROM chatwoot_forward_queue`).Scan(&queued) == nil && queued == 2
	}, 3*time.Second, 10*time.Millisecond)
	// Simulate a restart before the first Chatwoot delivery. Only committed
	// queue rows and media files survive.
	require.NoError(t, db.Close())
	_, reopened := openDB()
	var due []*domainChatStorage.ChatwootForwardEvent
	for range 2 {
		event, err := reopened.ClaimNextChatwootForwardEvent(time.Now(), time.Now().Add(time.Minute))
		require.NoError(t, err)
		require.NotNil(t, event)
		due = append(due, event)
	}
	payload := func(event *domainChatStorage.ChatwootForwardEvent, id string) map[string]any {
		t.Helper()
		require.Equal(t, sender.String(), event.DeviceID)
		require.Equal(t, id, event.WhatsAppMessageID)
		require.Empty(t, event.LastError)
		require.Zero(t, event.Attempts)
		var message whatsapp.WebhookEvent
		require.NoError(t, json.Unmarshal([]byte(event.PayloadJSON), &message))
		require.Equal(t, id, message.Payload["id"])
		require.Equal(t, true, message.Payload["is_from_me"])
		return message.Payload
	}
	data := payload(due[0], "file-ack")
	require.Equal(t, "Your receipt", data["body"])
	document, ok := data["document"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Receipt Copy.txt", document["filename"])
	path, ok := document["path"].(string)
	require.True(t, ok, "REST upload must be retained for replay")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(got))
	require.Equal(t, "Forward this", payload(due[1], "forward-ack")["body"])
}
