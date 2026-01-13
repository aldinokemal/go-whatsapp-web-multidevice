package whatsapp

import (
	"context"
	"time"

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
