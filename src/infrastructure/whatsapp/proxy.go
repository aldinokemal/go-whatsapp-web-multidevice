package whatsapp

import "net/url"

// redactProxyURL returns a form of the proxy URL safe for logging: the
// password (and any unparseable URL entirely) is masked so credentials from
// WHATSAPP_PROXY never reach the logs. A parse without a host counts as
// unparseable: "user:pass@host:1080" reads as scheme "user" with the
// credentials left in Opaque, which Redacted would print unchanged.
func redactProxyURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "<unparseable-proxy-url>"
	}
	return u.Redacted()
}
