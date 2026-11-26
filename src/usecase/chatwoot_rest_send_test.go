package usecase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
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
	chatUsecaseRepoStub
	queued chan *domainChatStorage.ChatwootForwardEvent
}

func (r *restMirrorRepo) GetMessageByIDAndDevice(_, id string) (*domainChatStorage.Message, error) {
	return &domainChatStorage.Message{ID: id, Content: "Forward this"}, nil
}

func (r *restMirrorRepo) StoreSentMessageWithContext(context.Context, string, string, string, string, time.Time, *waE2E.Message) error {
	return nil
}

func (r *restMirrorRepo) GetChatwootDeviceConfigByIdentifier(string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	return nil, errors.New("temporary destination lookup failure")
}

func (r *restMirrorRepo) EnqueueChatwootForwardEvent(event *domainChatStorage.ChatwootForwardEvent) error {
	r.queued <- event
	return nil
}

func TestChatwootMirrorFromRESTEndpoints(t *testing.T) {
	enabled, events := config.ChatwootEnabled, config.ChatwootEvents
	auto, limit, media := config.WhatsappAutoDownloadMedia, config.WhatsappSettingMaxDownloadSize, config.PathMedia
	t.Cleanup(func() {
		config.ChatwootEnabled, config.ChatwootEvents = enabled, events
		config.WhatsappAutoDownloadMedia, config.WhatsappSettingMaxDownloadSize, config.PathMedia = auto, limit, media
		chatwoot.InitClientRegistry(nil)
	})
	config.ChatwootEnabled, config.ChatwootEvents = true, []string{"message"}
	config.WhatsappAutoDownloadMedia, config.WhatsappSettingMaxDownloadSize, config.PathMedia = false, 1, t.TempDir()

	repo := &restMirrorRepo{queued: make(chan *domainChatStorage.ChatwootForwardEvent, 1)}
	chatwoot.InitClientRegistry(repo)
	sender := types.NewJID("441111111111", types.DefaultUserServer)
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
	send := func(req *http.Request, id string) map[string]any {
		t.Helper()
		resp, err := app.Test(req)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		select {
		case event := <-repo.queued:
			require.Equal(t, sender.String(), event.DeviceID)
			require.Equal(t, id, event.WhatsAppMessageID)
			var message whatsapp.WebhookEvent
			require.NoError(t, json.Unmarshal([]byte(event.PayloadJSON), &message))
			require.Equal(t, id, message.Payload["id"])
			require.Equal(t, true, message.Payload["is_from_me"])
			return message.Payload
		case <-time.After(3 * time.Second):
			t.Fatalf("acknowledged send %q did not reach the Chatwoot retry queue", id)
			return nil
		}
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
	data := send(req, "file-ack")
	require.Equal(t, "Your receipt", data["body"])
	document, ok := data["document"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Receipt Copy.txt", document["filename"])
	path, ok := document["path"].(string)
	require.True(t, ok, "REST upload must be retained for replay")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, content, string(got))

	req = httptest.NewRequest(http.MethodPost, "/message/original/forward", strings.NewReader(`{"phone":"442222222222"}`))
	req.Header.Set("Content-Type", "application/json")
	require.Equal(t, "Forward this", send(req, "forward-ack")["body"])
}
