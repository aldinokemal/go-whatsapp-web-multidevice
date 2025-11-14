package whatsapp

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/sirupsen/logrus"
)

func createSessionUpdatePayload(evt any, event string) map[string]any {
	body := make(map[string]any)

	body["payload"] = evt
	body["event"] = event

	return body
}

func forwardSessionUpdateToWebhook(ctx context.Context, evtType string, evt any) error {
	logrus.Infof("Forwarding %s event to %d configured webhook(s)", evtType, len(config.WhatsappWebhook))
	payload := createSessionUpdatePayload(evt, "session."+evtType)

	for _, url := range config.WhatsappWebhook {
		if err := submitWebhook(ctx, payload, url); err != nil {
			return err
		}
	}

	logrus.Infof("%s event forwarded to webhook", evtType)
	return nil
}
