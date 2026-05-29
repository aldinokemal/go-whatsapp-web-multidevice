package cmd

import (
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/helpers"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

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
// idle device briefly reads as not-connected. Banned vs disconnected is not
// distinguished here (both surface as "not working"); a future events.LoggedOut
// handler can split them.
func botStatusForSaas() (status string, phone string) {
	dm := whatsapp.GetDeviceManager()
	if dm == nil {
		return "disconnected", ""
	}
	inst := dm.DefaultDevice()
	if inst == nil {
		// 0 devices (never paired / logged out) or >1 (not single-device mode).
		return "pairing", ""
	}

	phone = inst.PhoneNumber()
	if phone == "" {
		phone = phoneFromJID(inst.JID())
	}
	if phone != "" && !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}

	if inst.IsLoggedIn() {
		return "active", phone
	}
	if inst.JID() != "" {
		return "disconnected", phone
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

// startAutoReconnectCheckerIfClientAvailable guards the reconnect checker behind a valid client reference.
func startAutoReconnectCheckerIfClientAvailable() {
	client := getValidWhatsAppClient()
	if client == nil {
		logrus.Warn("whatsapp client is nil; auto-reconnect checker not started")
		return
	}
	go helpers.SetAutoReconnectChecking(client)
}
