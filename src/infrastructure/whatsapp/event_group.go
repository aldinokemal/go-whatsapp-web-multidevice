package whatsapp

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// createGroupInfoPayload creates a webhook payload for group information events
func createGroupInfoPayload(evt *events.GroupInfo, actionType string, jids []types.JID) map[string]any {
	body := make(map[string]any)

	// Create payload structure matching the expected format
	payload := make(map[string]any)

	// Add group chat ID
	payload["chat_id"] = evt.JID.String()

	// Add action type and affected users
	payload["type"] = actionType
	payload["jids"] = jidsToStrings(jids)

	// Wrap in payload structure
	body["payload"] = payload

	// Add metadata for webhook processing
	body["event"] = "group.participants"
	body["timestamp"] = evt.Timestamp.Format(time.RFC3339)

	return body
}

// jidsToStrings converts a slice of JIDs to a slice of strings
func jidsToStrings(jids []types.JID) []string {
	if len(jids) == 0 {
		return []string{} // Return empty array instead of nil for consistent JSON
	}

	result := make([]string, len(jids))
	for i, jid := range jids {
		result[i] = jid.String()
	}
	return result
}

// forwardGroupInfoToWebhook forwards group information events to the configured webhook URLs
func forwardGroupInfoToWebhook(ctx context.Context, evt *events.GroupInfo) error {
	webhookService := GetWebhookService()
	if webhookService == nil {
		return nil
	}

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
			payload := createGroupInfoPayload(evt, action.actionType, action.jids)

			// Use webhook service to submit the event
			if err := webhookService.SubmitWebhook(ctx, "group."+action.actionType, payload); err != nil {
				logrus.Warnf("Failed to submit group %s webhook: %v", action.actionType, err)
			}

			logrus.Infof("Group %s event forwarded to webhook: %d users %s", action.actionType, len(action.jids), action.actionType)
		}
	}

	return nil
}
