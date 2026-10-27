package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

// Webhook events for the account restrictions WhatsApp reports to a device. Without
// them a temporary ban only shows as a disconnected device, and a reach-out timelock
// only shows as failed sends.
const (
	EventTypeDeviceLoggedOut        = "device.logged_out"
	EventTypeDeviceTemporaryBan     = "device.temporary_ban"
	EventTypeDeviceReachoutTimelock = "device.reachout_timelock"
)

// reasonText drops the "<code>: " prefix whatsmeow puts on its reason strings, since
// the code is sent in its own field.
func reasonText(code int, s string) string {
	return strings.TrimPrefix(s, fmt.Sprintf("%d: ", code))
}

func loggedOutPayload(evt *events.LoggedOut) map[string]any {
	return map[string]any{
		"code":       int(evt.Reason),
		"reason":     reasonText(int(evt.Reason), evt.Reason.String()),
		"on_connect": evt.OnConnect,
	}
}

func temporaryBanPayload(evt *events.TemporaryBan, now time.Time) map[string]any {
	payload := map[string]any{
		"code":               int(evt.Code),
		"reason":             reasonText(int(evt.Code), evt.Code.String()),
		"expires_in_seconds": int64(evt.Expire / time.Second),
	}
	if evt.Expire > 0 {
		payload["expires_at"] = now.Add(evt.Expire).UTC().Format(time.RFC3339)
	}
	return payload
}

func reachoutTimelockPayload(evt *events.NotifyAccountReachoutTimelock) map[string]any {
	payload := map[string]any{
		"is_active": evt.IsActive,
	}
	if evt.EnforcementType != "" {
		payload["enforcement_type"] = evt.EnforcementType
	}
	if !evt.TimeEnforcementEnds.IsZero() {
		payload["ends_at"] = evt.TimeEnforcementEnds.UTC().Format(time.RFC3339)
	}
	return payload
}

func handleTemporaryBan(ctx context.Context, instance *DeviceInstance, evt *events.TemporaryBan) {
	logrus.Warnf("[TEMPORARY_BAN] Device %s: %s", instance.ID(), evt)

	payload := temporaryBanPayload(evt, time.Now())
	broadcastDeviceEvent("DEVICE_TEMPORARY_BAN", evt.String(), instance.ID(), payload)
	forwardDeviceEvent(ctx, EventTypeDeviceTemporaryBan, instance.JID(), payload)
}

func handleReachoutTimelock(ctx context.Context, instance *DeviceInstance, evt *events.NotifyAccountReachoutTimelock) {
	payload := reachoutTimelockPayload(evt)

	message := "Reach-out timelock lifted"
	if evt.IsActive {
		message = "Reach-out timelock active: WhatsApp is restricting this account from starting new chats"
		if endsAt, ok := payload["ends_at"]; ok {
			message = fmt.Sprintf("%s until %s", message, endsAt)
		}
		logrus.Warnf("[REACHOUT_TIMELOCK] Device %s: %s (enforcement type %q)", instance.ID(), message, evt.EnforcementType)
	} else {
		logrus.Infof("[REACHOUT_TIMELOCK] Device %s: %s", instance.ID(), message)
	}

	broadcastDeviceEvent("DEVICE_REACHOUT_TIMELOCK", message, instance.ID(), payload)
	forwardDeviceEvent(ctx, EventTypeDeviceReachoutTimelock, instance.JID(), payload)
}

// broadcastDeviceEvent tells the websocket clients, adding the device_id the UI routes by.
func broadcastDeviceEvent(code, message, deviceID string, payload map[string]any) {
	result := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		result[key] = value
	}
	result["device_id"] = deviceID

	websocket.Broadcast <- websocket.BroadcastMessage{
		Code:    code,
		Message: message,
		Result:  result,
	}
}

// forwardDeviceEvent sends a device event to the webhooks. The device's webhook config
// and session id are looked up by its JID before this returns, because a remote logout
// clears that JID right after (keep-slot reset). Only the delivery runs in the background.
func forwardDeviceEvent(ctx context.Context, eventName, deviceJID string, payload map[string]any) {
	body := map[string]any{
		"event":     eventName,
		"payload":   payload,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	if deviceJID != "" {
		body["device_id"] = deviceJID
	}
	webhookConfig := resolveWebhookConfig(deviceJID)
	addWebhookSessionID(body)

	go func() {
		webhookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := forwardPayloadWithWebhookConfig(webhookCtx, body, eventName, webhookConfig); err != nil {
			logrus.Errorf("Failed to forward %s event to webhook: %v", eventName, err)
		}
	}()
}
