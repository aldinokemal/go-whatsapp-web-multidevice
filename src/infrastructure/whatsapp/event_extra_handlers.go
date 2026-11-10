package whatsapp

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	eventTypeAppState      = "app_state"
	eventTypeContactUpdate = "contact.update"
	eventTypeCallAccept    = "call.accept"
	eventTypeCallTerminate = "call.terminate"
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
