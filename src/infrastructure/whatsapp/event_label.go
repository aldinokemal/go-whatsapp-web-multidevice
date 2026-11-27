package whatsapp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	eventTypeLabelEdit        = "label.edit"
	eventTypeLabelAssociation = "label.association"
)

func isLabelAppState(evt *events.AppState) bool {
	if evt == nil || len(evt.Index) == 0 {
		return false
	}

	// Note: IndexLabelAssociationMessage (per-message label) is intentionally excluded.
	// Only chat-level associations and label edits are forwarded.
	return evt.Index[0] == appstate.IndexLabelEdit || evt.Index[0] == appstate.IndexLabelAssociationChat
}

func forwardLabelAppStateToWebhook(ctx context.Context, evt *events.AppState, deviceID string, client *whatsmeow.Client) error {
	eventName, payload := buildLabelAppStatePayload(ctx, evt, client)
	if eventName == "" {
		return nil
	}

	body := map[string]any{
		"event":     eventName,
		"timestamp": labelAppStateTimestamp(evt),
		"payload":   payload,
	}
	if deviceID != "" {
		body["device_id"] = deviceID
	}

	return forwardPayloadToConfiguredWebhooks(ctx, body, eventName)
}

func buildLabelAppStatePayload(ctx context.Context, evt *events.AppState, client *whatsmeow.Client) (string, map[string]any) {
	if evt == nil || evt.SyncActionValue == nil || len(evt.Index) == 0 {
		return "", nil
	}

	switch evt.Index[0] {
	case appstate.IndexLabelEdit:
		if len(evt.Index) < 2 || evt.LabelEditAction == nil {
			return "", nil
		}

		payload := map[string]any{"label_id": evt.Index[1]}
		addLabelEditActionFields(payload, evt.LabelEditAction)

		return eventTypeLabelEdit, payload
	case appstate.IndexLabelAssociationChat:
		if len(evt.Index) < 3 || evt.LabelAssociationAction == nil {
			return "", nil
		}

		payload := map[string]any{
			"label_id": evt.Index[1],
			"labeled":  evt.LabelAssociationAction.GetLabeled(),
		}
		addLabelChatFields(ctx, client, payload, evt.Index[2])

		return eventTypeLabelAssociation, payload
	default:
		return "", nil
	}
}

func addLabelEditActionFields(payload map[string]any, action *waSyncAction.LabelEditAction) {
	if action.Name != nil {
		payload["name"] = action.GetName()
	}
	if action.Color != nil {
		payload["color"] = action.GetColor()
	}
	if action.PredefinedID != nil {
		payload["predefined_id"] = action.GetPredefinedID()
	}
	if action.Deleted != nil {
		payload["deleted"] = action.GetDeleted()
	}
	if action.OrderIndex != nil {
		payload["order_index"] = action.GetOrderIndex()
	}
	if action.IsActive != nil {
		payload["is_active"] = action.GetIsActive()
	}
	if action.Type != nil {
		payload["type"] = action.GetType().String()
	}
	if action.IsImmutable != nil {
		payload["is_immutable"] = action.GetIsImmutable()
	}
	if action.MuteEndTimeMS != nil {
		payload["mute_end_time_ms"] = action.GetMuteEndTimeMS()
	}
}

func addLabelChatFields(ctx context.Context, client *whatsmeow.Client, payload map[string]any, rawJID string) {
	jid, err := types.ParseJID(rawJID)
	if err != nil {
		payload["chat_id"] = rawJID

		return
	}

	chatJID := jid.ToNonAD()
	if chatJID.Server == "lid" {
		payload["chat_lid"] = chatJID.String()
		if client != nil {
			chatJID = NormalizeJIDFromLID(ctx, chatJID, client).ToNonAD()
		}
	}

	payload["chat_id"] = chatJID.String()
}

func labelAppStateTimestamp(evt *events.AppState) string {
	if ts := evt.GetTimestamp(); ts > 0 {
		return time.UnixMilli(ts).UTC().Format(time.RFC3339)
	}

	return time.Now().UTC().Format(time.RFC3339)
}

// storeLabelAppState applies a label_edit or label_jid mutation to the label
// store of the device whose client received it. Chats keep the JID of the app
// state index, so a later unlabel of the same index finds the row; readers
// resolve LIDs to phone JIDs.
func storeLabelAppState(evt *events.AppState, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) error {
	if evt == nil || evt.SyncActionValue == nil || len(evt.Index) < 2 || chatStorageRepo == nil {
		return nil
	}
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return nil
	}
	deviceID := client.Store.ID.ToNonAD().String()
	labelID := evt.Index[1]

	switch evt.Index[0] {
	case appstate.IndexLabelEdit:
		action := evt.LabelEditAction
		if action == nil {
			return nil
		}
		if action.GetDeleted() {
			return chatStorageRepo.DeleteLabel(deviceID, labelID)
		}
		label := &domainChatStorage.Label{
			DeviceID:     deviceID,
			LabelID:      labelID,
			Name:         action.GetName(),
			Color:        action.GetColor(),
			PredefinedID: action.GetPredefinedID(),
			OrderIndex:   action.GetOrderIndex(),
			IsActive:     action.GetIsActive(),
			IsImmutable:  action.GetIsImmutable(),
		}
		if action.Type != nil {
			label.Type = action.GetType().String()
		}
		return chatStorageRepo.UpsertLabel(label)
	case appstate.IndexLabelAssociationChat:
		if len(evt.Index) < 3 || evt.LabelAssociationAction == nil {
			return nil
		}
		chatJID := evt.Index[2]
		if jid, err := types.ParseJID(chatJID); err == nil {
			chatJID = jid.ToNonAD().String()
		}
		if !evt.LabelAssociationAction.GetLabeled() {
			return chatStorageRepo.DeleteLabelAssociation(deviceID, labelID, chatJID)
		}
		return chatStorageRepo.UpsertLabelAssociation(&domainChatStorage.LabelAssociation{
			DeviceID: deviceID,
			LabelID:  labelID,
			ChatJID:  chatJID,
		})
	default:
		return nil
	}
}

var labelResyncMu sync.Mutex

// labelStoreFailures counts label mutations that could not be stored, for
// every device. A resync compares it around the fetch.
var labelStoreFailures atomic.Int64

// ErrLabelStoreIncomplete means a resync fetched the labels but at least one
// of them was not stored, so the store must not be pruned against it.
var ErrLabelStoreIncomplete = errors.New("label store was not fully rebuilt")

// FetchLabelAppState re-downloads the regular app state patch, which carries
// label_edit and label_jid, and dispatches every mutation in it as an event,
// so the label store is rebuilt through the normal event path before it
// returns.
//
// whatsmeow drops the events of a full sync unless EmitAppStateEventsOnFullSync
// is set, so the flag is set for this call only. whatsmeow reads it only for
// full syncs and app state recovery, never for the incremental syncs that
// run after pairing.
func FetchLabelAppState(ctx context.Context, client *whatsmeow.Client) error {
	if client == nil {
		return pkgError.ErrWaCLI
	}
	labelResyncMu.Lock()
	defer labelResyncMu.Unlock()

	previous := client.EmitAppStateEventsOnFullSync
	client.EmitAppStateEventsOnFullSync = true
	defer func() { client.EmitAppStateEventsOnFullSync = previous }()

	return fetchLabelAppState(func() error {
		return client.FetchAppState(ctx, appstate.WAPatchRegular, true, false)
	})
}

// fetchLabelAppState runs a label fetch and fails when a label mutation could
// not be stored while it ran. The counter is shared by all devices, so a
// failure on another device at the same moment also fails this sync; the
// caller then keeps its rows and the sync can be repeated.
func fetchLabelAppState(fetch func() error) error {
	failuresBefore := labelStoreFailures.Load()
	if err := fetch(); err != nil {
		return err
	}
	if labelStoreFailures.Load() != failuresBefore {
		return ErrLabelStoreIncomplete
	}
	return nil
}
