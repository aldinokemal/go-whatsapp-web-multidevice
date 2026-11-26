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

var labelLocks sync.Map // storage device ID -> *sync.Mutex

// LockLabels serializes the label writes of one device: app state events, API
// edits and a resync. It returns the unlock function.
func LockLabels(deviceID string) func() {
	value, _ := labelLocks.LoadOrStore(deviceID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// storeLabelAppState keeps chat storage in step with a label_edit or
// label_jid mutation, including the echo of the patches this device sends.
// An app state value replaces the previous one for its index, so the label
// row is written whole.
func storeLabelAppState(ctx context.Context, evt *events.AppState, deviceID string, client *whatsmeow.Client, repo domainChatStorage.IChatStorageRepository) error {
	if repo == nil || deviceID == "" || evt == nil || evt.SyncActionValue == nil || len(evt.Index) < 2 {
		return nil
	}
	defer LockLabels(deviceID)()
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
		return StoreLabelChat(repo, deviceID, evt.Index[1], stored, sent, evt.LabelAssociationAction.GetLabeled(), appStateTime(evt))
	}
	return nil
}

// StoreLabelChat records a chat added to or removed from a label under the
// stored JID. A row saved under the LID before it resolved is dropped, so one
// chat is never listed twice. The caller holds LockLabels.
func StoreLabelChat(repo domainChatStorage.IChatStorageRepository, deviceID, labelID, stored, sent string, labeled bool, at time.Time) error {
	if err := repo.SetLabelChat(deviceID, labelID, stored, labeled, at); err != nil {
		return err
	}
	if sent != "" && sent != stored {
		return repo.SetLabelChat(deviceID, labelID, sent, false, at)
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

// ResyncLabels runs a full sync of the "regular" app state collection, where
// labels and their chats live, and replaces the stored copy of the device with
// what it holds. Labels created before this device was paired only arrive this
// way. The device's label lock is held from the fetch to the replacement, so
// an app state event or API edit that lands meanwhile is applied after it.
//
// whatsmeow returns the mutations of a full sync only while
// EmitAppStateEventsOnFullSync is set and has no per-call option, so the flag
// is set on this device's client for the length of the call. The events are
// read here, not dispatched, so a resync does not send label webhooks.
func ResyncLabels(ctx context.Context, client *whatsmeow.Client, deviceID string, repo domainChatStorage.IChatStorageRepository) error {
	if client == nil {
		return fmt.Errorf("whatsapp client is not initialized")
	}
	if repo == nil || deviceID == "" {
		return fmt.Errorf("device identification required")
	}
	defer LockLabels(deviceID)()

	previous := client.EmitAppStateEventsOnFullSync
	client.EmitAppStateEventsOnFullSync = true
	evts, err := client.DangerousInternals().FetchAppState(ctx, appstate.WAPatchRegular, true, false)
	client.EmitAppStateEventsOnFullSync = previous
	if err != nil {
		return fmt.Errorf("failed to sync labels: %w", err)
	}

	labels, chats := labelStateFromEvents(ctx, client, deviceID, evts)
	if err := repo.ReplaceLabels(deviceID, labels, chats); err != nil {
		return fmt.Errorf("failed to store labels: %w", err)
	}
	logrus.Debugf("[LABEL] Full sync for %s stored %d labels and %d label chats", deviceID, len(labels), len(chats))
	return nil
}
