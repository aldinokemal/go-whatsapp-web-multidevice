package multiuser

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/netguard"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/stretchr/testify/require"
)

// Security: multi-user mode puts netguard on the send-by-URL, link-preview and
// webhook paths. Each real caller is driven at a loopback server and must be
// refused without the server seeing a request.
func TestApplyGuardsUserSteeredFetches(t *testing.T) {
	savedU, savedW := utils.OutboundClient, whatsapp.OutboundClient
	t.Cleanup(func() { utils.OutboundClient, whatsapp.OutboundClient = savedU, savedW })
	savedCfg := struct {
		mcp, oauth, chatwoot, ui, autoDL bool
		hooks, basic                     []string
		reply                            string
	}{config.McpEnabled, config.McpOAuthEnabled, config.ChatwootEnabled, config.AppUIEnabled, config.WhatsappAutoDownloadMedia,
		config.WhatsappWebhook, config.AppBasicAuthCredential, config.WhatsappAutoReplyMessage}
	t.Cleanup(func() {
		config.McpEnabled, config.McpOAuthEnabled, config.ChatwootEnabled = savedCfg.mcp, savedCfg.oauth, savedCfg.chatwoot
		config.AppUIEnabled, config.WhatsappAutoDownloadMedia = savedCfg.ui, savedCfg.autoDL
		config.WhatsappWebhook, config.AppBasicAuthCredential = savedCfg.hooks, savedCfg.basic
		config.WhatsappAutoReplyMessage = savedCfg.reply
	})

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	Apply()
	require.NotNil(t, utils.OutboundClient)
	require.NotNil(t, whatsapp.OutboundClient)

	_, _, err := utils.DownloadImageFromURL(srv.URL + "/a.png")
	require.ErrorIs(t, err, netguard.ErrRefused, "send image by URL")
	_, _, err = utils.DownloadFileFromURL(srv.URL + "/a.pdf")
	require.ErrorIs(t, err, netguard.ErrRefused, "send file by URL")
	_, err = utils.GetMetaDataFromURL(srv.URL + "/page")
	require.ErrorIs(t, err, netguard.ErrRefused, "link preview")

	_, err = whatsapp.OutboundClient(0, false).Post(srv.URL, "application/json", nil)
	require.ErrorIs(t, err, netguard.ErrRefused, "webhook delivery client")
	_, err = whatsapp.OutboundClient(0, true).Get("http://169.254.169.254/latest/meta-data/")
	require.ErrorIs(t, err, netguard.ErrRefused, "webhook with TLS checks off is still address-checked")

	require.Zero(t, hits, "the loopback server was never reached")
}
