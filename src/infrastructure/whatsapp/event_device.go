package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
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
	forwardDeviceEvent(ctx, EventTypeDeviceTemporaryBan, instance, payload)
	broadcastDeviceEvent("DEVICE_TEMPORARY_BAN", evt.String(), instance.ID(), payload)
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

	forwardDeviceEvent(ctx, EventTypeDeviceReachoutTimelock, instance, payload)
	broadcastDeviceEvent("DEVICE_REACHOUT_TIMELOCK", message, instance.ID(), payload)
}

// deviceBroadcastTimeout bounds how long a device event waits for the websocket hub,
// which writes to each browser in turn with no deadline.
var deviceBroadcastTimeout = 5 * time.Second

// broadcastDeviceEvent tells the websocket clients, adding the device_id the UI routes by.
// A hub stalled on a slow browser drops the message rather than holding the event loop.
func broadcastDeviceEvent(code, message, deviceID string, payload map[string]any) {
	result := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		result[key] = value
	}
	result["device_id"] = deviceID

	select {
	case websocket.Broadcast <- websocket.BroadcastMessage{Code: code, Message: message, Result: result}:
	case <-time.After(deviceBroadcastTimeout):
		logrus.Warnf("Dropped websocket %s broadcast for device %s: the hub did not take it within %s", code, deviceID, deviceBroadcastTimeout)
	}
}

// forwardDeviceEvent sends a device event to the webhooks. The webhook config and session
// id come from the emitting slot and are read before this returns, because a remote logout
// clears its JID right after (keep-slot reset). Only the delivery runs in the background.
func forwardDeviceEvent(ctx context.Context, eventName string, instance *DeviceInstance, payload map[string]any) {
	body := map[string]any{
		"event":      eventName,
		"payload":    payload,
		"timestamp":  time.Now().Format(time.RFC3339),
		"session_id": instance.ID(),
	}
	if jid := instance.JID(); jid != "" {
		body["device_id"] = jid
	}
	webhookConfig := instanceWebhookConfig(instance)

	go func() {
		webhookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := forwardPayloadWithWebhookConfig(webhookCtx, body, eventName, webhookConfig); err != nil {
			logrus.Errorf("Failed to forward %s event to webhook: %v", eventName, err)
		}
	}()
}

// instanceWebhookConfig resolves the slot's webhook config by its exact companion JID
// first: two slots on one number share the bare JID, which the record lookup rejects as
// ambiguous. The bare JID is the fallback for records saved without a companion JID, and
// only counts when the record it finds is this slot's: a sibling whose record holds the
// shared JID would otherwise lend this slot its webhook. Anything else uses the global config.
func instanceWebhookConfig(instance *DeviceInstance) *domainChatStorage.DeviceWebhookConfig {
	if adJID := instance.ADJID(); adJID != "" {
		if webhookConfig := resolveWebhookConfig(adJID); webhookConfig != nil {
			return webhookConfig
		}
	}

	jid := instance.JID()
	if jid == "" {
		return nil
	}
	record, err := getDeviceRecordForTest(jid)
	if err != nil {
		logrus.Warnf("Failed to get webhook config for device %s, falling back to global config: %v", jid, err)
		return nil
	}
	if record == nil || record.DeviceID != instance.ID() {
		return nil
	}
	return webhookConfigFromRecord(record)
}
