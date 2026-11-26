package whatsapp

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const restSendSlotKey = "rest_send_slot"

type restSendKey struct{}
type uploadedMedia map[string][]byte

// WithChatwootRESTSend opts one immediate REST send into outgoing mirroring.
// The scheduler, MCP and Chatwoot's own reply handler share the usecase but
// do not carry this request context when they dispatch a message.
func WithChatwootRESTSend(ctx context.Context) context.Context {
	if !config.ChatwootEnabled || !ChatwootReceivesMessages() {
		return ctx
	}
	return context.WithValue(ctx, restSendKey{}, make(uploadedMedia))
}

// RememberChatwootUpload keeps the actual (possibly transcoded) bytes until the
// send is acknowledged. A carousel can upload several files in one request.
func RememberChatwootUpload(ctx context.Context, hash, data []byte) {
	if uploads, ok := ctx.Value(restSendKey{}).(uploadedMedia); ok && len(hash) > 0 {
		uploads[string(hash)] = data
	}
}

// A matching REST upload is already local. Only remote media needs the
// operator's permission to download it from WhatsApp.
func canExtractWebhookMedia(ctx context.Context, file whatsmeow.DownloadableMessage) bool {
	if config.WhatsappAutoDownloadMedia {
		return true
	}
	uploads, _ := ctx.Value(restSendKey{}).(uploadedMedia)
	_, found := uploads[string(file.GetFileSHA256())]
	return found
}

func extractWebhookMedia(ctx context.Context, client *whatsmeow.Client, file whatsmeow.DownloadableMessage) (utils.ExtractedMedia, error) {
	if uploads, ok := ctx.Value(restSendKey{}).(uploadedMedia); ok {
		if data, found := uploads[string(file.GetFileSHA256())]; found {
			return utils.StoreMediaBytes(config.PathMedia, file, data)
		}
	}
	return utils.ExtractMedia(ctx, client, config.PathMedia, file)
}

func shouldMirrorRESTSend(ctx context.Context, recipient types.JID, msg *waE2E.Message, messageID types.MessageID) bool {
	_, optedIn := ctx.Value(restSendKey{}).(uploadedMedia)
	if !optedIn || messageID == "" || recipient.Server == types.BroadcastServer ||
		recipient.Server == types.NewsletterServer || utils.IsSystemBroadcastJID(recipient.String()) {
		return false
	}
	// REST uses the original view-once envelope as the wire format for
	// ordinary interactive cards and buttons, even without a privacy flag.
	// Other view-once envelopes and individually flagged media are private.
	for msg.GetEphemeralMessage() != nil {
		msg = msg.GetEphemeralMessage().GetMessage()
	}
	if wrapped := msg.GetViewOnceMessage(); wrapped != nil {
		return wrapped.GetMessage().GetInteractiveMessage() != nil
	}
	return msg != nil && msg.GetViewOnceMessageV2() == nil && msg.GetViewOnceMessageV2Extension() == nil &&
		!msg.GetImageMessage().GetViewOnce() && !msg.GetVideoMessage().GetViewOnce() &&
		!msg.GetAudioMessage().GetViewOnce() && !msg.GetPtvMessage().GetViewOnce()
}

// ForwardRESTSendToChatwoot mirrors the server-acknowledged message. whatsmeow
// emits no local Message event for SendMessage. Use only the Chatwoot leg.
func ForwardRESTSendToChatwoot(ctx context.Context, client *whatsmeow.Client, recipient types.JID, msg *waE2E.Message, sent whatsmeow.SendResponse) {
	if !shouldMirrorRESTSend(ctx, recipient, msg, sent.ID) {
		return
	}
	device, ok := DeviceFromContext(ctx)
	if !ok || device == nil || client == nil || client.Store == nil {
		return
	}
	sender := client.Store.GetJID().ToNonAD()
	if sender.IsEmpty() {
		return
	}
	// Save the mirror while the HTTP request still owns its form fields and
	// upload bytes. The queue worker performs the Chatwoot HTTP requests.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	deviceID := sender.String()
	route, err := getChatwootClientFn(deviceID)
	if errors.Is(err, chatwoot.ErrClientRegistryUnavailable) {
		logrus.Errorf("Chatwoot: cannot queue sent message %s: %v", sent.ID, err)
		return
	}
	if err == nil && (route == nil || route.Client == nil || !route.Client.IsConfigured()) {
		return
	}
	repo := device.GetChatStorage()
	if repo == nil {
		logrus.Errorf("Chatwoot: cannot queue sent message %s without chat storage", sent.ID)
		return
	}
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: recipient, Sender: sender, IsFromMe: true, IsGroup: recipient.Server == types.GroupServer},
			ID:            sent.ID, Timestamp: sent.Timestamp,
		},
		Message: msg,
	}
	aliases := map[string]any{}
	addChatwootIdentityAliases(aliases, sent.Chat, sent.Sender, types.EmptyJID, types.EmptyJID)
	buildFromFields(ctx, client, evt, aliases)
	if chatwootPayloadExcluded(aliases) {
		return
	}
	poll := preparePollWebhookPayload(ctx, client, nil, evt)
	event, data, err := buildEventPayload(ctx, client, evt, poll)
	uploads, _ := ctx.Value(restSendKey{}).(uploadedMedia)
	clear(uploads)
	if err != nil {
		logrus.Errorf("Chatwoot: failed to build sent message %s: %v", evt.Info.ID, utils.MediaErrorForLog(err))
		return
	}
	// The storage path has a sanitized basename. Keep the document's display
	// name separately through queue serialization for the multipart upload.
	if path := extractMediaPath(data["document"]); path != "" {
		data["document"] = map[string]any{"path": path, "filename": utils.UnwrapMessage(evt.Message).GetDocumentMessage().GetFileName()}
	}
	maps.Copy(data, aliases)
	payload := map[string]any{"event": event, "device_id": deviceID, "payload": data, restSendSlotKey: device.ID()}
	if storeChatwootForwardEvent(repo, deviceID, event, sent.ID, payload, time.Now(), nil) {
		wakeChatwootForwardWorker()
	}
}
