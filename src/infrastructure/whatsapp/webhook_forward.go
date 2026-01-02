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

// BroadcastWebhookEvent is the standard method for broadcasting events to all configured webhooks.
func BroadcastWebhookEvent(ctx context.Context, eventName string, payload map[string]any) error {
	if !shouldForwardEvent(eventName) {
		return nil
	}

	webhooks := config.WhatsappWebhook
	if len(webhooks) == 0 {
		logrus.Debugf("No webhook configured for %s; skipping dispatch", eventName)
		return nil
	}

	logrus.Infof("Forwarding %s to %d configured webhook(s)", eventName, len(webhooks))

	failedErrors, successCount := dispatchToWebhooks(ctx, webhooks, payload, eventName)

	return handleBroadcastResults(eventName, len(webhooks), successCount, failedErrors)
}

func shouldForwardEvent(eventName string) bool {
	if len(config.WhatsappWebhookEvents) > 0 {
		if !isEventWhitelisted(eventName) {
			logrus.Debugf("Skipping event %s - not in webhook events whitelist", eventName)
			return false
		}
	}
	return true
}

func dispatchToWebhooks(ctx context.Context, webhooks []string, payload map[string]any, eventName string) ([]string, int) {
	var failedErrors []string
	var successCount int

	for _, url := range webhooks {
		if err := submitWebhookFn(ctx, payload, url); err != nil {
			failedErrors = append(failedErrors, fmt.Sprintf("%s: %v", url, err))
			logrus.Warnf("Failed forwarding %s to %s: %v", eventName, url, err)
			continue
		}
		successCount++
	}
	return failedErrors, successCount
}

func handleBroadcastResults(eventName string, total int, success int, failed []string) error {
	if len(failed) == total {
		return pkgError.WebhookError(fmt.Sprintf("all webhook URLs failed for %s: %s", eventName, strings.Join(failed, "; ")))
	}

	if len(failed) > 0 {
		logrus.Warnf("Some webhook URLs failed for %s (succeeded: %d/%d): %s", eventName, success, total, strings.Join(failed, "; "))
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
