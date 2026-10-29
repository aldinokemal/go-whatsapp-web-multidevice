package cmd

import (
	"net"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// loadHTTPHardeningConfig applies env and .env values for settings that are
// not wired in root.go. An explicit flag wins over the environment.
func loadHTTPHardeningConfig() {
	flags := rootCmd.PersistentFlags()
	if flag := flags.Lookup("allow-unauthenticated"); flag == nil || !flag.Changed {
		if viper.IsSet("app_allow_unauthenticated") {
			config.AppAllowUnauthenticated = viper.GetBool("app_allow_unauthenticated")
		}
	}
	if flag := flags.Lookup("chatwoot-webhook-allow-unsigned"); flag == nil || !flag.Changed {
		if viper.IsSet("chatwoot_webhook_allow_unsigned") {
			config.ChatwootWebhookAllowUnsigned = viper.GetBool("chatwoot_webhook_allow_unsigned")
		}
	}
	if flag := flags.Lookup("trusted-origins"); flag == nil || !flag.Changed {
		if raw := strings.TrimSpace(viper.GetString("app_trusted_origins")); raw != "" {
			config.AppTrustedOrigins = splitComma(raw)
		}
	}
}

func splitComma(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ensureWebhookSigningSecret() {
	replaced, err := config.UseEphemeralWebhookSecret()
	if err != nil {
		logrus.Fatalf("webhook signing secret: %v", err)
	}
	if !replaced {
		return
	}
	logrus.Warn("WHATSAPP_WEBHOOK_SECRET is unset or still a published value (\"secret\", \"super-secret-key\" or \"your-private-key\"). Generated a random per-process signing key. It will change on restart, so set WHATSAPP_WEBHOOK_SECRET to a stable secret before receivers rely on signatures. Events stay signed; the published value is not used.")
}

func enforceAuthenticationPolicy() {
	if basicAuthComplete(config.AppBasicAuthCredential) {
		return
	}
	if config.AppAllowUnauthenticated {
		logrus.Warn("APP_ALLOW_UNAUTHENTICATED is enabled: device administration, send, logout, webhook changes, and MCP routes are exposed without application authentication. Use this only behind a reverse proxy that authenticates clients.")
		return
	}
	if isLoopbackHost(config.AppHost) {
		logrus.Warn("APP_BASIC_AUTH is empty or has an entry with an empty username or password, and the listener is loopback (" + config.AppHost + "). The API is unauthenticated for local development. Do not publish this listener. A non-loopback bind requires APP_BASIC_AUTH or APP_ALLOW_UNAUTHENTICATED=true.")
		return
	}
	logrus.Fatalf("APP_BASIC_AUTH is empty or has an entry with an empty username or password, and APP_HOST %q is not a loopback address. Refusing to expose device administration, send, logout, and webhook routes. Set APP_BASIC_AUTH, bind 127.0.0.1, or set APP_ALLOW_UNAUTHENTICATED=true when a reverse proxy performs authentication.", config.AppHost)
}

// basicAuthComplete reports whether APP_BASIC_AUTH protects the API: it has
// at least one entry, and every entry has a username and a password. An entry
// such as "admin:" would admit anyone who knows the username.
func basicAuthComplete(credentials []string) bool {
	for _, credential := range credentials {
		user, password, ok := strings.Cut(credential, ":")
		if !ok || user == "" || password == "" {
			return false
		}
	}
	return len(credentials) > 0
}

// unauthenticatedBindAllowed reports whether an empty credential list may
// expose the API. Loopback binds stay open for local development. Any other
// bind requires credentials or the explicit insecure switch.
func unauthenticatedBindAllowed(host string, hasCredentials, allowInsecure bool) bool {
	return hasCredentials || allowInsecure || isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
