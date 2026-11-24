package multiuser

import (
	"net/http"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/netguard"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
)

const (
	downloadRedirects = 5
	webhookRedirects  = 3
	webhookMaxBody    = 1 << 20 // the response is never read; this only bounds a hostile one
)

// installOutboundGuard routes every server-side fetch a user can steer through
// netguard: send-by-URL and link previews (pkg/utils) and per-device webhook
// delivery (infrastructure/whatsapp). Only multi-user mode calls it, so
// upstream single-user behaviour is unchanged.
func installOutboundGuard() {
	utils.OutboundClient = func(timeout time.Duration) *http.Client {
		// The helpers apply their own, tighter, per-type caps; this is the
		// ceiling for anything that reads without one (link previews).
		return netguard.NewClient(netguard.Options{
			Timeout:      timeout,
			MaxRedirects: downloadRedirects,
			MaxBody:      maxDownloadBody(),
		})
	}
	whatsapp.OutboundClient = func(timeout time.Duration, insecureSkipVerify bool) *http.Client {
		return netguard.NewClient(netguard.Options{
			Timeout:            timeout,
			MaxRedirects:       webhookRedirects,
			MaxBody:            webhookMaxBody,
			InsecureSkipVerify: insecureSkipVerify,
		})
	}
}

func maxDownloadBody() int64 {
	m := config.WhatsappSettingMaxDownloadSize
	for _, v := range []int64{config.WhatsappSettingMaxFileSize, config.WhatsappSettingMaxImageSize, config.WhatsappSettingMaxVideoSize} {
		if v > m {
			m = v
		}
	}
	return m
}
