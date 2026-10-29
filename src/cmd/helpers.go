package cmd

import (
	"context"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/helpers"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
)

// initChatwootForwarding wires the WhatsApp->Chatwoot forward path shared by the
// REST and MCP servers. Both servers connect the WhatsApp client and run the
// same event pipeline, so both must initialize the registry or forwards silently
// drop.
//
// Order matters: the per-device client registry is installed BEFORE the retry
// worker starts. The worker runs its first pass immediately, and a nil registry
// would make a due retry resolve to "no client" and be marked done (deleted)
// without delivery. Initializing first closes that race; the env inbox
// auto-create (when enabled) runs ahead of both so the legacy client has its
// inbox id resolved before any forward when no per-device configs exist.
func initChatwootForwarding(repo domainChatStorage.IChatStorageRepository) {
	if !config.ChatwootEnabled {
		return
	}
	if config.ChatwootAutoCreate {
		count, err := repo.CountChatwootDeviceConfigs()
		if err != nil {
			logrus.Errorf("Chatwoot auto-create skipped: failed to count per-device configs: %v", err)
		} else if count == 0 {
			if err := chatwoot.EnsureInbox(chatwoot.GetDefaultClient()); err != nil {
				logrus.Errorf("Chatwoot auto-create failed: %v", err)
			}
		}
	}
	// Stamp the env account id onto pre-migration legacy links (account id 0) so
	// the reverse route resolves them by exact account instead of the legacy-zero
	// wildcard — closing a cross-account misroute once a second account is added.
	if config.ChatwootAccountID != 0 {
		if n, err := repo.BackfillChatwootMessageLinkAccount(config.ChatwootAccountID); err != nil {
			logrus.Errorf("Chatwoot: failed to backfill legacy message-link account ids: %v", err)
		} else if n > 0 {
			logrus.Infof("Chatwoot: backfilled %d legacy message link(s) to account %d", n, config.ChatwootAccountID)
		}
	}
	chatwoot.InitClientRegistry(repo)
	whatsapp.StartChatwootForwardRetryWorker(repo)
}

var presencePulseSchedulerOnce sync.Once

// getValidWhatsAppClient returns an initialized WhatsApp client if available.
func getValidWhatsAppClient() *whatsmeow.Client {
	client := whatsappCli
	if client == nil {
		client = whatsapp.GetClient()
	}
	return client
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
