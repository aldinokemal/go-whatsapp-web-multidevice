package whatsapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/sirupsen/logrus"
)

var submitWebhookFn = submitWebhook

// forwardPayloadToConfiguredWebhooks attempts to deliver the provided payload to every configured webhook URL.
// It only returns an error when all webhook deliveries fail. Partial failures are logged and suppressed so
// successful targets still receive the event.
func forwardPayloadToConfiguredWebhooks(ctx context.Context, payload map[string]any, eventName string) error {
	// Check if event is whitelisted (if whitelist is configured)
	if len(config.WhatsappWebhookEvents) > 0 {
		if !isEventWhitelisted(eventName) {
			logrus.Debugf("Skipping event %s - not in webhook events whitelist", eventName)
			return nil
		}
	}

	total := len(config.WhatsappWebhook)
	logrus.Infof("Forwarding %s to %d configured webhook(s)", eventName, total)

	if total == 0 {
		logrus.Infof("No webhook configured for %s; skipping dispatch", eventName)
		return nil
	}

	var (
		failed    []string
		successes int
	)
	for _, url := range config.WhatsappWebhook {
		if err := submitWebhookFn(ctx, payload, url); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", url, err))
			logrus.Warnf("Failed forwarding %s to %s: %v", eventName, url, err)
			continue
		}
		successes++
	}

	if len(failed) == total {
		return pkgError.WebhookError(fmt.Sprintf("all webhook URLs failed for %s: %s", eventName, strings.Join(failed, "; ")))
	}

	if len(failed) > 0 {
		logrus.Warnf("Some webhook URLs failed for %s (succeeded: %d/%d): %s", eventName, successes, total, strings.Join(failed, "; "))
	} else {
		logrus.Infof("%s forwarded to all webhook(s)", eventName)
	}

	return nil
}

// isEventWhitelisted checks if the given event name is in the configured whitelist
func isEventWhitelisted(eventName string) bool {
	for _, allowed := range config.WhatsappWebhookEvents {
		if strings.EqualFold(strings.TrimSpace(allowed), eventName) {
			return true
		}
	}
	return false
}
