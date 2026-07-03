package whatsapp

import "net/url"

// redactProxyURL returns a form of the proxy URL safe for logging: the
// password (and any unparseable URL entirely) is masked so credentials from
// WHATSAPP_PROXY never reach the logs.
func redactProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable-proxy-url>"
	}
	return u.Redacted()
}
