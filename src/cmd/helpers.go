package cmd

import (
	"context"
	"strings"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/helpers"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

var presencePulseSchedulerOnce sync.Once

// getValidWhatsAppClient returns an initialized WhatsApp client if available.
func getValidWhatsAppClient() *whatsmeow.Client {
	client := whatsappCli
	if client == nil {
		client = whatsapp.GetClient()
	}
	return client
}

// botStatusForSaas derives the SaaS bot status from the device manager for the
// status heartbeat. "active" tracks LOGGED-IN (paired) state, not the transient
// socket — an idle paired bot is still active, so this avoids the flap where an
// idle device briefly reads as not-connected.
//
// It scans ALL device records (not DefaultDevice, which needs exactly one): a
// re-pair can leave a stale logged-out record next to the live one, and as long
// as ANY device is logged in the bot is working. Banned vs disconnected is not
// distinguished here (both surface as "not working"); a future events.LoggedOut
// handler can split them.
func botStatusForSaas() (status string, phone string) {
	dm := whatsapp.GetDeviceManager()
	if dm == nil {
		return "disconnected", ""
	}
	devices := dm.ListDevices()
	if len(devices) == 0 {
		return "pairing", ""
	}

	var fallbackJID string
	for _, inst := range devices {
		if inst == nil {
			continue
		}
		if inst.IsLoggedIn() {
			p := inst.PhoneNumber()
			if p == "" {
				p = phoneFromJID(inst.JID())
			}
			return "active", normalizePhone(p)
		}
		if inst.JID() != "" {
			fallbackJID = inst.JID()
		}
	}

	// No device logged in. Distinguish "paired but offline" from "never paired".
	if fallbackJID != "" {
		return "disconnected", normalizePhone(phoneFromJID(fallbackJID))
	}
	return "pairing", ""
}

// phoneFromJID extracts the bare phone digits from a WhatsApp JID such as
// "5215661985644:5@s.whatsapp.net" → "5215661985644".
func phoneFromJID(jid string) string {
	if i := strings.IndexAny(jid, ":@"); i >= 0 {
		return jid[:i]
	}
	return jid
}

// normalizePhone ensures a leading "+" on a bare digit string.
func normalizePhone(p string) string {
	if p != "" && !strings.HasPrefix(p, "+") {
		return "+" + p
	}
	return p
}

// startAutoReconnectCheckerIfClientAvailable guards the reconnect checker behind a valid client reference.
func startAutoReconnectCheckerIfClientAvailable() {
	client := getValidWhatsAppClient()
	if client == nil {
		logrus.Warn("whatsapp client is nil; auto-reconnect checker not started")
		return
	}
	go helpers.SetAutoReconnectChecking(client)
}

// startPresencePulseSchedulerIfEnabled starts the process-wide presence pulse scheduler once.
func startPresencePulseSchedulerIfEnabled() {
	if !config.WhatsappPresencePulseEnabled {
		logrus.Info("presence pulse scheduler disabled")
		return
	}

	dm := whatsapp.GetDeviceManager()
	if dm == nil {
		logrus.Warn("device manager is nil; presence pulse scheduler not started")
		return
	}

	presencePulseSchedulerOnce.Do(func() {
		whatsapp.StartPresencePulseScheduler(
			context.Background(),
			dm,
			config.WhatsappPresencePulseInterval,
			config.WhatsappPresencePulseDuration,
		)
		logrus.Infof("presence pulse scheduler started; interval=%s duration=%s", config.WhatsappPresencePulseInterval, config.WhatsappPresencePulseDuration)
	})
}
