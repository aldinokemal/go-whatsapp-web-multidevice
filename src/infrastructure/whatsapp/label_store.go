package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// LabelFromAction builds the stored state of a label from a label_edit
// mutation. A label without an is_active flag counts as active.
func LabelFromAction(deviceID, labelID string, action *waSyncAction.LabelEditAction, updatedAt time.Time) *domainChatStorage.Label {
	label := &domainChatStorage.Label{
		DeviceID:   deviceID,
		ID:         labelID,
		Name:       action.GetName(),
		Color:      action.GetColor(),
		OrderIndex: action.GetOrderIndex(),
		Active:     true,
		Deleted:    action.GetDeleted(),
		UpdatedAt:  updatedAt,
	}
	if action.Type != nil {
		label.Type = action.GetType().String()
	}
	if action.IsActive != nil {
		label.Active = action.GetIsActive()
	}
	return label
}

// labelChatJIDs returns the chat JID a label link is stored under (the phone
// JID when a LID resolves) and, when it differs, the JID as WhatsApp sent it.
func labelChatJIDs(ctx context.Context, raw string, client *whatsmeow.Client) (stored, sent string, ok bool) {
	jid, err := types.ParseJID(raw)
	if err != nil || jid.IsEmpty() {
		return "", "", false
	}
	chat := jid.ToNonAD()
	sent = chat.String()
	if chat.Server == types.HiddenUserServer && client != nil {
		chat = NormalizeJIDFromLID(ctx, chat, client).ToNonAD()
	}
	return chat.String(), sent, true
}

func appStateTime(evt *events.AppState) time.Time {
	if ts := evt.GetTimestamp(); ts > 0 {
		return time.UnixMilli(ts).UTC()
	}
	return time.Now().UTC()
}

// storeLabelAppState keeps chat storage in step with a label_edit or
// label_jid mutation, including the echo of the patches this device sends.
func storeLabelAppState(ctx context.Context, evt *events.AppState, deviceID string, client *whatsmeow.Client, repo domainChatStorage.IChatStorageRepository) error {
	if repo == nil || deviceID == "" || evt == nil || evt.SyncActionValue == nil || len(evt.Index) < 2 {
		return nil
	}
	switch evt.Index[0] {
	case appstate.IndexLabelEdit:
		if evt.LabelEditAction == nil {
			return nil
		}
		return repo.UpsertLabel(LabelFromAction(deviceID, evt.Index[1], evt.LabelEditAction, appStateTime(evt)))
	case appstate.IndexLabelAssociationChat:
		if len(evt.Index) < 3 || evt.LabelAssociationAction == nil {
			return nil
		}
		stored, sent, ok := labelChatJIDs(ctx, evt.Index[2], client)
		if !ok {
			return nil
		}
		labeled := evt.LabelAssociationAction.GetLabeled()
		if err := repo.SetLabelChat(deviceID, evt.Index[1], stored, labeled, appStateTime(evt)); err != nil {
			return err
		}
		// A link saved under the LID before it resolved goes away with the chat.
		if !labeled && sent != stored {
			return repo.SetLabelChat(deviceID, evt.Index[1], sent, false, appStateTime(evt))
		}
	}
	return nil
}

// labelStateFromEvents collects the labels and label links of a full app
// state sync. Later mutations of the same index win.
func labelStateFromEvents(ctx context.Context, client *whatsmeow.Client, deviceID string, evts []any) ([]*domainChatStorage.Label, []*domainChatStorage.LabelChat) {
	labels := map[string]*domainChatStorage.Label{}
	var labelOrder []string
	type link struct{ labelID, chatJID string }
	links := map[link]*domainChatStorage.LabelChat{}
	var linkOrder []link

	for _, raw := range evts {
		evt, ok := raw.(*events.AppState)
		if !ok || evt.SyncActionValue == nil || len(evt.Index) < 2 {
			continue
		}
		switch evt.Index[0] {
		case appstate.IndexLabelEdit:
			if evt.LabelEditAction == nil {
				continue
			}
			if _, seen := labels[evt.Index[1]]; !seen {
				labelOrder = append(labelOrder, evt.Index[1])
			}
			labels[evt.Index[1]] = LabelFromAction(deviceID, evt.Index[1], evt.LabelEditAction, appStateTime(evt))
		case appstate.IndexLabelAssociationChat:
			if len(evt.Index) < 3 || evt.LabelAssociationAction == nil {
				continue
			}
			stored, _, ok := labelChatJIDs(ctx, evt.Index[2], client)
			if !ok {
				continue
			}
			key := link{labelID: evt.Index[1], chatJID: stored}
			if !evt.LabelAssociationAction.GetLabeled() {
				delete(links, key)
				continue
			}
			if _, seen := links[key]; !seen {
				linkOrder = append(linkOrder, key)
			}
			links[key] = &domainChatStorage.LabelChat{DeviceID: deviceID, LabelID: key.labelID, ChatJID: key.chatJID, UpdatedAt: appStateTime(evt)}
		}
	}

	outLabels := make([]*domainChatStorage.Label, 0, len(labelOrder))
	for _, id := range labelOrder {
		outLabels = append(outLabels, labels[id])
	}
	outLinks := make([]*domainChatStorage.LabelChat, 0, len(links))
	for _, key := range linkOrder {
		if chat, ok := links[key]; ok {
			outLinks = append(outLinks, chat)
			delete(links, key) // a link removed and added again is listed once
		}
	}
	return outLabels, outLinks
}

var labelResyncMu sync.Mutex

// FetchLabelState runs a full sync of the "regular" app state collection,
// where labels and their chats live, and returns what it holds. Labels created
// before this device was paired only arrive this way.
//
// whatsmeow returns the mutations of a full sync only while
// EmitAppStateEventsOnFullSync is set and has no per-call option, so the flag
// is set for the length of the call. The events are read here, not
// dispatched, so a resync does not send label webhooks.
func FetchLabelState(ctx context.Context, client *whatsmeow.Client, deviceID string) ([]*domainChatStorage.Label, []*domainChatStorage.LabelChat, error) {
	if client == nil {
		return nil, nil, fmt.Errorf("whatsapp client is not initialized")
	}
	labelResyncMu.Lock()
	defer labelResyncMu.Unlock()

	previous := client.EmitAppStateEventsOnFullSync
	client.EmitAppStateEventsOnFullSync = true
	defer func() { client.EmitAppStateEventsOnFullSync = previous }()

	evts, err := client.DangerousInternals().FetchAppState(ctx, appstate.WAPatchRegular, true, false)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to sync labels: %w", err)
	}
	labels, chats := labelStateFromEvents(ctx, client, deviceID, evts)
	logrus.Debugf("[LABEL] Full sync for %s returned %d labels and %d label chats", deviceID, len(labels), len(chats))
	return labels, chats, nil
}
