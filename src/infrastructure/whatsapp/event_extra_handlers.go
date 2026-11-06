package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	eventTypeAppState      = "app_state"
	eventTypeContactUpdate = "contact.update"
	eventTypeCallAccept    = "call.accept"
	eventTypeCallTerminate = "call.terminate"
	eventTypeGroupInfo     = "group.info"
	eventTypeGroupLeft     = "group.left"
	eventTypeHistorySync   = "history.sync"
	eventTypeConnection    = "connection"
	eventTypeQR            = "qr"
)

// A history.sync body stays well under the 1 MB request limit common to webhook receivers.
const (
	historySyncMaxMessages  = 500
	historySyncMaxBodyBytes = 900_000
	historySyncSendTimeout  = 60 * time.Second
)

func forwardAppState(ctx context.Context, evt any, deviceID string, client *whatsmeow.Client) {
	forwardExtraEvent(ctx, eventTypeAppState, func(ctx context.Context) map[string]any {
		return appStatePayload(ctx, evt, deviceID, client)
	})
}

func forwardContactUpdate(ctx context.Context, evt any, deviceID string, client *whatsmeow.Client) {
	forwardExtraEvent(ctx, eventTypeContactUpdate, func(ctx context.Context) map[string]any {
		return contactUpdatePayload(ctx, evt, deviceID, client)
	})
}

func forwardCallAccept(ctx context.Context, evt *events.CallAccept, deviceID string, client *whatsmeow.Client) {
	forwardExtraEvent(ctx, eventTypeCallAccept, func(ctx context.Context) map[string]any {
		return callAcceptPayload(ctx, evt, deviceID, client)
	})
}

func forwardCallTerminate(ctx context.Context, evt *events.CallTerminate, deviceID string, client *whatsmeow.Client) {
	forwardExtraEvent(ctx, eventTypeCallTerminate, func(ctx context.Context) map[string]any {
		return callTerminatePayload(ctx, evt, deviceID, client)
	})
}

// forwardExtraEvent builds the body off the event loop, since LID resolution hits the store.
func forwardExtraEvent(ctx context.Context, eventName string, build func(context.Context) map[string]any) {
	go func() {
		webhookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		body := build(webhookCtx)
		if body == nil {
			return
		}
		if err := forwardPayloadToConfiguredWebhooks(webhookCtx, body, eventName); err != nil {
			logrus.Errorf("Failed to forward %s event to webhook: %v", eventName, err)
		}
	}()
}

// appStatePayload returns nil for events emitted by a full app state sync: those replay the
// whole chat list right after pairing and are a snapshot, not a change made by the user.
func appStatePayload(ctx context.Context, rawEvt any, deviceID string, client *whatsmeow.Client) map[string]any {
	payload := make(map[string]any)
	var (
		chat      types.JID
		timestamp time.Time
	)

	switch evt := rawEvt.(type) {
	case *events.Archive:
		if evt.FromFullSync {
			return nil
		}
		chat, timestamp = evt.JID, evt.Timestamp
		payload["action"] = "archive"
		if evt.Action != nil && evt.Action.Archived != nil {
			payload["value"] = evt.Action.GetArchived()
		}
	case *events.Pin:
		if evt.FromFullSync {
			return nil
		}
		chat, timestamp = evt.JID, evt.Timestamp
		payload["action"] = "pin"
		if evt.Action != nil && evt.Action.Pinned != nil {
			payload["value"] = evt.Action.GetPinned()
		}
	case *events.Mute:
		if evt.FromFullSync {
			return nil
		}
		chat, timestamp = evt.JID, evt.Timestamp
		payload["action"] = "mute"
		if evt.Action != nil && evt.Action.Muted != nil {
			payload["value"] = evt.Action.GetMuted()
		}
		// Milliseconds since epoch; -1 means muted forever.
		if end := evt.Action.GetMuteEndTimestamp(); evt.Action.GetMuted() && end > 0 {
			payload["until"] = time.UnixMilli(end).UTC().Format(time.RFC3339)
		}
	case *events.ClearChat:
		if evt.FromFullSync {
			return nil
		}
		chat, timestamp = evt.JID, evt.Timestamp
		payload["action"] = "clear"
	case *events.DeleteChat:
		if evt.FromFullSync {
			return nil
		}
		chat, timestamp = evt.JID, evt.Timestamp
		payload["action"] = "delete_chat"
	case *events.Contact:
		if evt.FromFullSync {
			return nil
		}
		chat, timestamp = evt.JID, evt.Timestamp
		payload["action"] = "contact"
		contact := make(map[string]any)
		if name := evt.Action.GetFullName(); name != "" {
			contact["full_name"] = name
		}
		if name := evt.Action.GetFirstName(); name != "" {
			contact["first_name"] = name
		}
		if len(contact) > 0 {
			payload["contact"] = contact
		}
	default:
		return nil
	}

	if chat.Server == types.HiddenUserServer {
		payload["chat_lid"] = chat.ToNonAD().String()
	}
	payload["chat_id"] = resolveExtraJID(ctx, chat, client)

	return extraEventBody(eventTypeAppState, timestamp, deviceID, payload)
}

func contactUpdatePayload(ctx context.Context, rawEvt any, deviceID string, client *whatsmeow.Client) map[string]any {
	payload := make(map[string]any)
	var (
		user      types.JID
		timestamp time.Time
	)

	switch evt := rawEvt.(type) {
	case *events.PushName:
		user = evt.JID
		if evt.Message != nil {
			timestamp = evt.Message.Timestamp
		}
		payload["kind"] = "push_name"
		if evt.NewPushName != "" {
			payload["push_name"] = evt.NewPushName
		}
	case *events.Picture:
		user, timestamp = evt.JID, evt.Timestamp
		payload["kind"] = "picture"
		if evt.Remove {
			payload["picture_removed"] = true
		}
	case *events.UserAbout:
		user, timestamp = evt.JID, evt.Timestamp
		payload["kind"] = "about"
		if evt.Status != "" {
			payload["about"] = evt.Status
		}
	case *events.BusinessName:
		user = evt.JID
		if evt.Message != nil {
			timestamp = evt.Message.Timestamp
		}
		payload["kind"] = "business_name"
		if evt.NewBusinessName != "" {
			payload["business_name"] = evt.NewBusinessName
		}
	default:
		return nil
	}

	addExtraSender(ctx, payload, user, client)
	return extraEventBody(eventTypeContactUpdate, timestamp, deviceID, payload)
}

func callAcceptPayload(ctx context.Context, evt *events.CallAccept, deviceID string, client *whatsmeow.Client) map[string]any {
	payload := callMetaPayload(ctx, evt.BasicCallMeta, client)
	return extraEventBody(eventTypeCallAccept, evt.Timestamp, deviceID, payload)
}

func callTerminatePayload(ctx context.Context, evt *events.CallTerminate, deviceID string, client *whatsmeow.Client) map[string]any {
	payload := callMetaPayload(ctx, evt.BasicCallMeta, client)
	if evt.Reason != "" {
		payload["reason"] = evt.Reason
	}
	return extraEventBody(eventTypeCallTerminate, evt.Timestamp, deviceID, payload)
}

func callMetaPayload(ctx context.Context, meta types.BasicCallMeta, client *whatsmeow.Client) map[string]any {
	payload := map[string]any{"call_id": meta.CallID}
	addExtraSender(ctx, payload, meta.CallCreator, client)
	if !meta.GroupJID.IsEmpty() {
		payload["group_jid"] = meta.GroupJID.ToNonAD().String()
	}
	return payload
}

func addExtraSender(ctx context.Context, payload map[string]any, sender types.JID, client *whatsmeow.Client) {
	if sender.Server == types.HiddenUserServer {
		payload["from_lid"] = sender.ToNonAD().String()
	}
	payload["from"] = resolveExtraJID(ctx, sender, client)
}

func resolveExtraJID(ctx context.Context, jid types.JID, client *whatsmeow.Client) string {
	if client != nil {
		jid = NormalizeJIDFromLID(ctx, jid, client)
	}
	return jid.ToNonAD().String()
}

func extraEventBody(eventName string, timestamp time.Time, deviceID string, payload map[string]any) map[string]any {
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	body := map[string]any{
		"event":     eventName,
		"timestamp": timestamp.UTC().Format(time.RFC3339),
		"payload":   payload,
	}
	if deviceID != "" {
		body["device_id"] = deviceID
	}
	return body
}

func forwardGroupInfo(ctx context.Context, evt any, deviceID string, client *whatsmeow.Client) {
	switch e := evt.(type) {
	case *events.GroupInfo:
		forwardExtraEvent(ctx, eventTypeGroupInfo, func(ctx context.Context) map[string]any {
			return groupInfoPayload(ctx, e, deviceID, client)
		})
		if len(e.Leave) > 0 {
			forwardExtraEvent(ctx, eventTypeGroupLeft, func(ctx context.Context) map[string]any {
				return groupLeftPayload(ctx, e, deviceID, client)
			})
		}
	case *events.Picture:
		if e.JID.Server == types.GroupServer {
			forwardExtraEvent(ctx, eventTypeGroupInfo, func(ctx context.Context) map[string]any {
				return groupInfoPayload(ctx, e, deviceID, client)
			})
		}
	}
}

// groupInfoPayload carries only the settings present in the event. A non-nil setting is a
// change even when its new value is false or zero (announce, lock or timer turned off).
// Name changes are left out: forwardGroupInfoToWebhook already sends them as group.info.
func groupInfoPayload(ctx context.Context, rawEvt any, deviceID string, client *whatsmeow.Client) map[string]any {
	payload := make(map[string]any)
	var (
		group     types.JID
		timestamp time.Time
		sender    string
	)

	switch evt := rawEvt.(type) {
	case *events.GroupInfo:
		if evt.Topic != nil {
			payload["topic"] = evt.Topic.Topic
		}
		if evt.Announce != nil {
			payload["announce"] = evt.Announce.IsAnnounce
		}
		if evt.Locked != nil {
			payload["locked"] = evt.Locked.IsLocked
		}
		if evt.Ephemeral != nil {
			seconds := evt.Ephemeral.DisappearingTimer
			if !evt.Ephemeral.IsEphemeral {
				seconds = 0
			}
			payload["ephemeral_seconds"] = seconds
		}
		if len(payload) == 0 {
			return nil
		}
		group, timestamp = evt.JID, evt.Timestamp
		sender = groupEventSender(ctx, evt.Sender, evt.SenderPN, client)
	case *events.Picture:
		if evt.JID.Server != types.GroupServer {
			return nil
		}
		payload["picture_changed"] = true
		group, timestamp = evt.JID, evt.Timestamp
		sender = groupEventSender(ctx, &evt.Author, nil, client)
	default:
		return nil
	}

	payload["chat_id"] = group.ToNonAD().String()
	if sender != "" {
		payload["sender"] = sender
	}
	return extraEventBody(eventTypeGroupInfo, timestamp, deviceID, payload)
}

func groupLeftPayload(ctx context.Context, evt *events.GroupInfo, deviceID string, client *whatsmeow.Client) map[string]any {
	if client == nil || client.Store == nil || !slices.ContainsFunc(evt.Leave, func(jid types.JID) bool { return isOwnJID(ctx, jid, client) }) {
		return nil
	}
	payload := map[string]any{"chat_id": evt.JID.ToNonAD().String()}
	if sender := groupEventSender(ctx, evt.Sender, evt.SenderPN, client); sender != "" {
		payload["sender"] = sender
	}
	return extraEventBody(eventTypeGroupLeft, evt.Timestamp, deviceID, payload)
}

func groupEventSender(ctx context.Context, sender, senderPN *types.JID, client *whatsmeow.Client) string {
	if sender == nil || sender.IsEmpty() {
		return ""
	}
	if sender.Server == types.HiddenUserServer && senderPN != nil && !senderPN.IsEmpty() {
		return senderPN.ToNonAD().String()
	}
	return resolveExtraJID(ctx, *sender, client)
}

func isOwnJID(ctx context.Context, jid types.JID, client *whatsmeow.Client) bool {
	sameUser := func(a, b types.JID) bool { return a.User != "" && a.User == b.User && a.Server == b.Server }
	own := client.Store.ID
	if own != nil && sameUser(jid, *own) {
		return true
	}
	if sameUser(jid, client.Store.GetLID()) {
		return true
	}
	return own != nil && jid.Server == types.HiddenUserServer && sameUser(NormalizeJIDFromLID(ctx, jid, client), *own)
}

// forwardConnection reports a successful pairing. Logouts and temporary bans are already
// reported by the device.logged_out and device.temporary_ban events.
func forwardConnection(ctx context.Context, evt *events.PairSuccess, sessionID string) {
	now := time.Now()
	forwardExtraEvent(ctx, eventTypeConnection, func(context.Context) map[string]any {
		return connectionPayload(evt, sessionID, now)
	})
}

// connectionPayload sets session_id itself because the device JID does not map to its
// session until this event has been handled.
func connectionPayload(evt *events.PairSuccess, sessionID string, now time.Time) map[string]any {
	deviceID := evt.ID.ToNonAD().String()
	payload := map[string]any{"state": "pair_success", "phone": deviceID}
	body := extraEventBody(eventTypeConnection, now, deviceID, payload)
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	return body
}

// ForwardLoginQRCode forwards a login QR code to the configured webhooks as a "qr" event.
func ForwardLoginQRCode(ctx context.Context, instance *DeviceInstance, code string, timeout time.Duration) {
	if instance == nil {
		return
	}
	deviceID, sessionID := instance.JID(), instance.ID()
	forwardExtraEvent(ctx, eventTypeQR, func(context.Context) map[string]any {
		return qrPayload(code, timeout, deviceID, sessionID)
	})
}

func qrPayload(code string, timeout time.Duration, deviceID, sessionID string) map[string]any {
	payload := map[string]any{
		"code":            code,
		"timeout_seconds": int(timeout / time.Second),
	}
	body := extraEventBody(eventTypeQR, time.Time{}, deviceID, payload)
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	return body
}

type historySyncConversation struct {
	chatID   string
	name     string
	messages []map[string]any
}

func (c historySyncConversation) entry() map[string]any {
	entry := map[string]any{"chat_id": c.chatID, "messages": []map[string]any{}}
	if c.name != "" {
		entry["name"] = c.name
	}
	return entry
}

// historySyncLocks holds one mutex per device: a fresh pairing delivers many large syncs back
// to back, and each device sends its history events one at a time without delaying others.
var historySyncLocks sync.Map

// forwardHistorySync sends the history in the background and one event at a time: a full
// sync can hold thousands of messages.
func forwardHistorySync(ctx context.Context, evt *events.HistorySync, deviceID string, client *whatsmeow.Client) {
	if evt == nil || evt.Data == nil {
		return
	}
	go func() {
		ctx := context.WithoutCancel(ctx)
		bodies := historySyncWebhookBodies(ctx, evt, deviceID, client)
		// The sync proto can be tens of megabytes; only the bodies must outlive the wait for the lock.
		evt = nil
		if len(bodies) == 0 {
			return
		}
		lock, _ := historySyncLocks.LoadOrStore(deviceID, &sync.Mutex{})
		lock.(*sync.Mutex).Lock()
		defer lock.(*sync.Mutex).Unlock()
		for _, body := range bodies {
			webhookCtx, cancel := context.WithTimeout(ctx, historySyncSendTimeout)
			if err := forwardPayloadToConfiguredWebhooks(webhookCtx, body, eventTypeHistorySync); err != nil {
				logrus.Errorf("Failed to forward %s event to webhook: %v", eventTypeHistorySync, err)
			}
			cancel()
		}
	}()
}

// historySyncWebhookBodies returns nothing, before parsing a single message, when no webhook
// takes history.sync: building a message payload can download its media.
func historySyncWebhookBodies(ctx context.Context, evt *events.HistorySync, deviceID string, client *whatsmeow.Client) []map[string]any {
	if !hasWebhookTargetForEvent(ctx, deviceID, eventTypeHistorySync) {
		return nil
	}
	return historySyncPayloads(ctx, evt, deviceID, client)
}

// hasWebhookTargetForEvent applies the delivery rules of forwardPayloadToConfiguredWebhooks to
// an event that carries no chat JID at the top level and never goes to Chatwoot.
func hasWebhookTargetForEvent(ctx context.Context, deviceID, eventName string) bool {
	deviceConfig, err := webhookConfigForPayload(ctx, map[string]any{"device_id": deviceID})
	if errors.Is(err, errAmbiguousWebhookDevice) {
		return false
	}
	if err != nil {
		deviceConfig = nil
	}
	urls := getWebhookURLsFromConfig(deviceConfig)
	if len(urls) == 0 {
		urls = config.WhatsappWebhook
	}
	if len(urls) > 0 && isEventWhitelistedForDevice(eventName, deviceConfig) {
		return true
	}
	return config.WhatsappWebhookDeviceMergeGlobal && deviceConfig != nil && len(config.WhatsappWebhook) > 0 && isEventWhitelistedForDevice(eventName, nil)
}

func historySyncPayloads(ctx context.Context, evt *events.HistorySync, deviceID string, client *whatsmeow.Client) []map[string]any {
	data := evt.Data
	var progress *uint32
	if data.Progress != nil {
		value := data.GetProgress()
		progress = &value
	}
	return historySyncBodies(deviceID, sessionIDForJIDFn(deviceID), data.GetSyncType().String(), progress, historySyncConversations(ctx, data, client), time.Now())
}

// historySyncConversations builds each message exactly like the live "message" event. Edits,
// revokes, reactions and other protocol messages are left out: a history replays content, and
// those only make sense as live changes. Status and broadcast chats are left out as on the
// live path. The chat and sender JIDs sit below the top level of the body, out of reach of the
// forwarder's ignore check, so ignored chats and senders are dropped here.
func historySyncConversations(ctx context.Context, data *waHistorySync.HistorySync, client *whatsmeow.Client) []historySyncConversation {
	if client == nil {
		return nil
	}
	var conversations []historySyncConversation
	for _, conv := range data.GetConversations() {
		chat, err := types.ParseJID(conv.GetID())
		if err != nil || chat.IsEmpty() || chat.Server == types.BroadcastServer {
			continue
		}
		entry := historySyncConversation{chatID: resolveExtraJID(ctx, chat, client), name: conv.GetName()}
		ignore := config.WhatsappWebhookIgnoreJids
		if utils.MatchesIgnoredJID(entry.chatID, ignore) || utils.MatchesIgnoredJID(chat.ToNonAD().String(), ignore) {
			continue
		}
		if entry.name == "" {
			entry.name = conv.GetDisplayName()
		}
		for _, histMsg := range conv.GetMessages() {
			webMsg := histMsg.GetMessage()
			if webMsg.GetMessage() == nil || utils.UnwrapMessage(webMsg.GetMessage()).GetProtocolMessage() != nil {
				continue
			}
			msgEvt, err := client.ParseWebMessage(chat, webMsg)
			if err != nil {
				logrus.Debugf("Skipping history sync message %s: failed to parse it", webMsg.GetKey().GetID())
				continue
			}
			eventType, payload, err := buildEventPayload(ctx, client, msgEvt)
			if err != nil {
				logrus.Debugf("Skipping history sync message %s: failed to build its payload", webMsg.GetKey().GetID())
				continue
			}
			if eventType == EventTypeMessage && !shouldIgnoreWebhookJID(map[string]any{"payload": payload}) {
				entry.messages = append(entry.messages, payload)
			}
		}
		if len(entry.messages) > 0 {
			conversations = append(conversations, entry)
		}
	}
	return conversations
}

// historySyncBodies splits the history into events of at most historySyncMaxMessages messages
// and less than historySyncMaxBodyBytes of JSON, keeping message order; a conversation may
// continue in the next event. Sizes are summed per message, which matches json.Marshal of the
// whole body: appending an element to a JSON array grows it by the element plus one comma.
// A message over the limit on its own is sent alone. session_id is set here, not by the
// forwarder, so that it is part of the measured size.
func historySyncBodies(deviceID, sessionID, syncType string, progress *uint32, conversations []historySyncConversation, now time.Time) []map[string]any {
	newBody := func(convs []map[string]any) map[string]any {
		payload := map[string]any{"sync_type": syncType, "conversations": convs}
		if progress != nil {
			payload["progress"] = *progress
		}
		body := extraEventBody(eventTypeHistorySync, now, deviceID, payload)
		if sessionID != "" {
			body["session_id"] = sessionID
		}
		return body
	}
	baseSize := jsonSize(newBody([]map[string]any{}))

	var (
		bodies  []map[string]any
		current []map[string]any
		size    = baseSize
		count   int
	)
	flush := func() {
		if count == 0 {
			return
		}
		bodies = append(bodies, newBody(current))
		current, size, count = nil, baseSize, 0
	}

	for _, conv := range conversations {
		headerSize := jsonSize(conv.entry())
		var entry map[string]any
		for _, msg := range conv.messages {
			msgSize := jsonSize(msg)
			added := msgSize + 1
			if entry == nil {
				added = msgSize + headerSize
				if len(current) > 0 {
					added++
				}
			}
			if count > 0 && (count >= historySyncMaxMessages || size+added >= historySyncMaxBodyBytes) {
				flush()
				entry = nil
				added = msgSize + headerSize
			}
			if entry == nil {
				entry = conv.entry()
				current = append(current, entry)
			}
			entry["messages"] = append(entry["messages"].([]map[string]any), msg)
			size += added
			count++
		}
	}
	flush()
	return bodies
}

func jsonSize(value any) int {
	raw, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return len(raw)
}
