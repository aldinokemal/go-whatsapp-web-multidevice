package whatsapp

import (
	"context"
	"fmt"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// createGroupInfoPayload creates a webhook payload for group information events
func createGroupInfoPayload(ctx context.Context, evt *events.GroupInfo, actionType string, jids []types.JID, deviceID string, client *whatsmeow.Client) map[string]any {
	body := make(map[string]any)

	// Create payload structure matching the expected format
	payload := make(map[string]any)

	// Add group chat ID (groups use @g.us, not @lid, so no LID resolution needed)
	payload["chat_id"] = evt.JID.ToNonAD().String()

	// Add action type and affected users (with LID resolution)
	payload["type"] = actionType
	payload["jids"] = jidsToStrings(ctx, jids, client)

	// Wrap in payload structure
	body["payload"] = payload

	// Add metadata for webhook processing
	body["event"] = "group.participants"
	body["timestamp"] = evt.Timestamp.Format(time.RFC3339)
	if deviceID != "" {
		body["device_id"] = deviceID
	}

	return body
}

// jidsToStrings converts a slice of JIDs to a slice of strings, resolving LIDs to phone numbers
func jidsToStrings(ctx context.Context, jids []types.JID, client *whatsmeow.Client) []string {
	if len(jids) == 0 {
		return []string{} // Return empty array instead of nil for consistent JSON
	}

	result := make([]string, len(jids))
	for i, jid := range jids {
		// Resolve LID to phone number if possible
		normalizedJID := NormalizeJIDFromLID(ctx, jid, client)
		result[i] = normalizedJID.ToNonAD().String()
	}
	return result
}

// forwardGroupInfoToWebhook forwards group information events to the configured webhook URLs
func forwardGroupInfoToWebhook(ctx context.Context, evt *events.GroupInfo, deviceID string, client *whatsmeow.Client) error {
	// Send separate webhook events for each action type
	actions := []struct {
		actionType string
		jids       []types.JID
	}{
		{"join", evt.Join},
		{"leave", evt.Leave},
		{"promote", evt.Promote},
		{"demote", evt.Demote},
	}

	for _, action := range actions {
		if len(action.jids) > 0 {
			payload := createGroupInfoPayload(ctx, evt, action.actionType, action.jids, deviceID, client)

			if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "group.participants"); err != nil {
				logrus.Warnf("Failed to forward group %s event to webhook: %v", action.actionType, err)
			}
		}
	}

	return nil
}

// handleJoinedGroup handles the event when the connected device is added to a new group
func handleJoinedGroup(ctx context.Context, evt *events.JoinedGroup, deviceID string, client *whatsmeow.Client) {
	log.Infof("Joined group %s (reason: %s, type: %s)", evt.JID, evt.Reason, evt.Type)

	if len(config.WhatsappWebhook) > 0 {
		go func(e *events.JoinedGroup, c *whatsmeow.Client) {
			webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := forwardJoinedGroupToWebhook(webhookCtx, e, deviceID, c); err != nil {
				logrus.Errorf("Failed to forward joined group event to webhook: %v", err)
			}
		}(evt, client)
	}
}

// forwardJoinedGroupToWebhook forwards the JoinedGroup event to configured webhooks
func forwardJoinedGroupToWebhook(ctx context.Context, evt *events.JoinedGroup, deviceID string, client *whatsmeow.Client) error {
	// Get own JID to include in the payload
	ownJID := client.Store.ID
	if ownJID == nil {
		return fmt.Errorf("client store ID is nil")
	}

	payload := map[string]any{
		"chat_id": evt.JID.ToNonAD().String(),
		"type":    "join",
		"jids":    []string{ownJID.ToNonAD().String()},
		"reason":  evt.Reason, // "invite" if via invite link
	}

	// Include group name if available (GroupName.Name is embedded in GroupInfo)
	if evt.GroupName.Name != "" {
		payload["group_name"] = evt.GroupName.Name
	}

	body := map[string]any{
		"event":     "group.joined",
		"payload":   payload,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	if deviceID != "" {
		body["device_id"] = deviceID
	}

	return forwardPayloadToConfiguredWebhooks(ctx, body, "group.joined")
}
