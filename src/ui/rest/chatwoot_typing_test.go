package rest

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

func TestResolveWhatsAppDestination(t *testing.T) {
	t.Run("waha_whatsapp_jid attribute (private)", func(t *testing.T) {
		dest, isGroup := resolveWhatsAppDestination(chatwoot.Contact{
			CustomAttributes: map[string]any{"waha_whatsapp_jid": "573166203787@s.whatsapp.net"},
		})
		if isGroup {
			t.Errorf("expected private chat, got group")
		}
		if dest == "" {
			t.Errorf("expected a destination, got empty")
		}
	})

	t.Run("phone number fallback", func(t *testing.T) {
		dest, isGroup := resolveWhatsAppDestination(chatwoot.Contact{PhoneNumber: "+573166203787"})
		if isGroup || dest == "" {
			t.Errorf("dest=%q isGroup=%v", dest, isGroup)
		}
	})

	t.Run("group jid", func(t *testing.T) {
		dest, isGroup := resolveWhatsAppDestination(chatwoot.Contact{
			CustomAttributes: map[string]any{"waha_whatsapp_jid": "123456789-987654@g.us"},
		})
		if !isGroup {
			t.Errorf("expected group, got private (dest=%q)", dest)
		}
		if dest == "" {
			t.Errorf("expected group destination, got empty")
		}
	})

	t.Run("no contact info", func(t *testing.T) {
		dest, _ := resolveWhatsAppDestination(chatwoot.Contact{})
		if dest != "" {
			t.Errorf("expected empty destination, got %q", dest)
		}
	})
}
