package chatwoot

import (
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/sirupsen/logrus"
)

// EnsureInbox provisions the Chatwoot inbox at startup when auto-create is
// enabled. It reuses an existing inbox whose name matches
// config.ChatwootInboxName — so restarts never pile up duplicates — and
// otherwise creates a fresh API-channel inbox wired to config.ChatwootWebhookURL.
// On success it sets config.ChatwootInboxID (and the passed client's InboxID)
// so both the REST client and the direct-DB importer target the resolved inbox.
//
// It is a no-op when auto-create is disabled, when the client is not minimally
// configured (URL + token + account id), or when an explicit CHATWOOT_INBOX_ID
// is already set — that value is treated as the operator's override.
func EnsureInbox(client *Client) error {
	if !config.ChatwootAutoCreate {
		return nil
	}
	if client == nil || client.BaseURL == "" || client.APIToken == "" || client.AccountID == 0 {
		logrus.Warn("Chatwoot auto-create: skipped — CHATWOOT_URL, CHATWOOT_API_TOKEN, and CHATWOOT_ACCOUNT_ID must all be set")
		return nil
	}
	if config.ChatwootInboxID != 0 {
		logrus.Infof("Chatwoot auto-create: CHATWOOT_INBOX_ID=%d already set; skipping provisioning", config.ChatwootInboxID)
		return nil
	}

	name := strings.TrimSpace(config.ChatwootInboxName)
	if name == "" {
		name = "WhatsApp"
	}

	inboxes, err := client.ListInboxes()
	if err != nil {
		return fmt.Errorf("chatwoot auto-create: list inboxes: %w", err)
	}
	// Reuse only an API-channel inbox of the configured name. A same-name inbox
	// of another (non-API) channel type, often also called "WhatsApp",
	// cannot receive our agent-reply webhook, so binding
	// to it would silently break outbound — skip it and create a dedicated API
	// inbox instead.
	for _, inbox := range inboxes {
		if !strings.EqualFold(inbox.Name, name) {
			continue
		}
		if !strings.EqualFold(inbox.ChannelType, "Channel::Api") {
			logrus.Warnf("Chatwoot auto-create: existing inbox %q (id=%d) is channel %q, not an API channel; creating a separate API inbox (set CHATWOOT_INBOX_ID to use a specific inbox)", inbox.Name, inbox.ID, inbox.ChannelType)
			continue
		}
		applyResolvedInbox(client, inbox)
		logrus.Infof("Chatwoot auto-create: reusing existing API inbox %q (id=%d)", inbox.Name, inbox.ID)
		return nil
	}

	created, err := client.CreateInbox(name, config.ChatwootWebhookURL)
	if err != nil {
		return fmt.Errorf("chatwoot auto-create: create inbox: %w", err)
	}
	applyResolvedInbox(client, *created)
	if config.ChatwootWebhookURL == "" {
		logrus.Warnf("Chatwoot auto-create: created inbox %q (id=%d) WITHOUT a webhook URL — set CHATWOOT_WEBHOOK_URL so Chatwoot agent replies reach WhatsApp", created.Name, created.ID)
	} else {
		logrus.Infof("Chatwoot auto-create: created inbox %q (id=%d) with webhook %s", created.Name, created.ID, config.ChatwootWebhookURL)
	}
	return nil
}

// applyResolvedInbox records the resolved inbox id on both the global config
// (read by the direct-DB importer and freshly-constructed clients) and the
// live client instance (whose InboxID was 0 before provisioning). The inbox
// identifier is cached too so read sync never needs a lookup GET to find it.
func applyResolvedInbox(client *Client, inbox Inbox) {
	config.ChatwootInboxID = inbox.ID
	client.InboxID = inbox.ID
	if inbox.InboxIdentifier != "" {
		client.InboxIdentifier = inbox.InboxIdentifier
	}
}
