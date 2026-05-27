package cmd

import (
	"context"
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
