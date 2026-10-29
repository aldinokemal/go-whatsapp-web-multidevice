package utils

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestRedactSettingsHidesSecretsAndKeepsPortHost(t *testing.T) {
	in := map[string]any{
		"app_port":                             "3000",
		"app_host":                             "0.0.0.0",
		"app_debug":                            false,
		"app_basic_auth":                       "user1:pass1,user2:pass2",
		"app_ui_github_token":                  "ghp_supersecret",
		"app_ui_asset_sha256":                  "abc123pin",
		"app_trusted_proxies":                  "10.0.0.0/8",
		"db_uri":                               "postgresql://dbuser:dbpass@db.internal:5432/whatsapp",
		"db_keys_uri":                          "postgresql://keyuser:keypass@keys.internal:5432/keys",
		"whatsapp_webhook":                     "https://hooks.example/a,https://user:pass@hooks.example/b",
		"whatsapp_webhook_secret":              "super-secret-key",
		"whatsapp_webhook_events":              "message,message.ack",
		"whatsapp_account_validation":          "true",
		"whatsapp_proxy":                       "socks5://user:pass@proxy.internal:1080",
		"chatwoot_url":                         "https://app.chatwoot.com",
		"chatwoot_api_token":                   "cw_token_value",
		"chatwoot_import_db_uri":               "postgresql://cw:cwpass@pg.internal:5432/chatwoot",
		"chatwoot_webhook_url":                 "https://public.example/chatwoot/webhook?secret=cw-wh-secret",
		"chatwoot_webhook_secret":              "cw-wh-secret",
		"chatwoot_webhook_signature_tolerance": "5m",
		"API_KEY":                              "plain-api-key",
		"service_password":                     "hunter2",
	}
	originalBasic := in["app_basic_auth"]
	originalDSN := in["db_uri"]

	got := RedactSettings(in)
	printed := fmt.Sprint(got)

	if in["app_basic_auth"] != originalBasic || in["db_uri"] != originalDSN {
		t.Fatal("RedactSettings mutated the input map")
	}
	if got["app_port"] != "3000" || got["app_host"] != "0.0.0.0" {
		t.Fatalf("ordinary port/host were hidden: port=%v host=%v", got["app_port"], got["app_host"])
	}
	if !strings.Contains(printed, "app_port:3000") || !strings.Contains(printed, "app_host:0.0.0.0") {
		t.Fatalf("startup print hid port/host: %s", printed)
	}
	if got["app_debug"] != false {
		t.Fatalf("non-secret bool changed: %#v", got["app_debug"])
	}
	if got["app_ui_asset_sha256"] != "abc123pin" {
		t.Fatalf("asset pin should stay visible, got %#v", got["app_ui_asset_sha256"])
	}
	if got["app_trusted_proxies"] != "10.0.0.0/8" {
		t.Fatalf("trusted proxy list should stay visible, got %#v", got["app_trusted_proxies"])
	}
	if got["whatsapp_account_validation"] != "true" {
		t.Fatalf("account validation should stay visible, got %#v", got["whatsapp_account_validation"])
	}
	if got["whatsapp_webhook_events"] != "message,message.ack" {
		t.Fatalf("webhook event list should stay visible, got %#v", got["whatsapp_webhook_events"])
	}
	if got["chatwoot_webhook_signature_tolerance"] != "5m" {
		t.Fatalf("signature tolerance should stay visible, got %#v", got["chatwoot_webhook_signature_tolerance"])
	}
	if got["chatwoot_url"] != "https://app.chatwoot.com" {
		t.Fatalf("chatwoot URL should stay visible, got %#v", got["chatwoot_url"])
	}
	if got["chatwoot_webhook_url"] != "https://public.example/chatwoot/webhook?secret=***" {
		t.Fatalf("chatwoot webhook URL should stay visible with its secret masked, got %#v", got["chatwoot_webhook_url"])
	}

	for _, key := range []string{
		"app_basic_auth",
		"app_ui_github_token",
		"db_uri",
		"db_keys_uri",
		"whatsapp_webhook_secret",
		"whatsapp_proxy",
		"chatwoot_api_token",
		"chatwoot_import_db_uri",
		"chatwoot_webhook_secret",
		"API_KEY",
		"service_password",
	} {
		if got[key] != redactedSecret {
			t.Fatalf("key %s = %#v, want %s", key, got[key], redactedSecret)
		}
	}

	webhook, _ := got["whatsapp_webhook"].(string)
	if webhook == redactedSecret {
		t.Fatal("non-secret webhook destinations were fully redacted")
	}
	if strings.Contains(webhook, "user:pass@") || strings.Contains(webhook, "pass@") {
		t.Fatalf("webhook userinfo leaked: %s", webhook)
	}
	if !strings.Contains(webhook, "https://hooks.example/a") || !strings.Contains(webhook, "https://***@hooks.example/b") {
		t.Fatalf("webhook URL redaction = %s", webhook)
	}

	for _, leaked := range []string{
		"pass1", "pass2", "ghp_supersecret", "dbpass", "keypass", "super-secret-key",
		"user:pass@", "cw_token_value", "cwpass", "cw-wh-secret", "plain-api-key", "hunter2",
	} {
		if strings.Contains(printed, leaked) {
			t.Fatalf("startup print leaked %q: %s", leaked, printed)
		}
	}
}

func TestRedactSettingsProxyWithoutUserinfoStaysVisible(t *testing.T) {
	got := RedactSettings(map[string]any{
		"whatsapp_proxy": "http://127.0.0.1:8080",
		"app_port":       "3000",
	})
	if got["whatsapp_proxy"] != "http://127.0.0.1:8080" {
		t.Fatalf("proxy without userinfo = %#v", got["whatsapp_proxy"])
	}
	if got["app_port"] != "3000" {
		t.Fatalf("port = %#v", got["app_port"])
	}
}

func TestRedactSettingsUnparseableProxyWithUserinfo(t *testing.T) {
	raw := "socks5://user:pass@proxy.internal:not-a-port"
	got := RedactSettings(map[string]any{"whatsapp_proxy": raw})
	printed := fmt.Sprint(got)
	if got["whatsapp_proxy"] != redactedSecret {
		t.Fatalf("malformed proxy = %#v", got["whatsapp_proxy"])
	}
	if strings.Contains(printed, "user:pass@") {
		t.Fatalf("malformed proxy leaked credentials: %s", printed)
	}
}

func TestRedactSettingsSchemelessProxyWithUserinfo(t *testing.T) {
	// url.Parse reads "user" as the scheme and leaves the credentials in Opaque.
	got := RedactSettings(map[string]any{"whatsapp_proxy": "user:pass@proxy.example:1080"})
	if got["whatsapp_proxy"] != redactedSecret {
		t.Fatalf("schemeless proxy = %#v, want %s", got["whatsapp_proxy"], redactedSecret)
	}
}

func TestRedactSettingsUserinfoWithQuoteOrComma(t *testing.T) {
	got := RedactSettings(map[string]any{
		"whatsapp_webhook": "https://user:p'ss@hooks.example/b",
		"chatwoot_url":     "https://agent:pa,ss@chat.example",
		"cache_url":        "redis://:p@ss,w0rd@cache:6379",
		"slash_url":        "https://user:pa/ss@hooks.example/b",
		"query_url":        "https://user:pa?ss@hooks.example",
		"fragment_url":     "https://user:pa#ss@hooks.example",
		"list_url":         "https://hooks.example/a,https://u:p/w@hooks.example/b c",
		"scheme_url":       "https://user:pahttps://ss@hooks.example",
		"spaced_url":       "https://a x=https://u:p@b",
		"quoted_url":       `https://a "https://u:p@b"`,
	})
	for key, want := range map[string]string{
		"whatsapp_webhook": "https://***@hooks.example/b",
		"chatwoot_url":     "https://***@chat.example",
		"cache_url":        "redis://***@cache:6379",
		"slash_url":        "https://***@hooks.example/b",
		"query_url":        "https://***@hooks.example",
		"fragment_url":     "https://***@hooks.example",
		"list_url":         "https://hooks.example/a,https://***@hooks.example/b c",
		"scheme_url":       "https://***@hooks.example",
		"spaced_url":       "https://a x=https://***@b",
		"quoted_url":       `https://a "https://***@b"`,
	} {
		if got[key] != want {
			t.Fatalf("%s = %#v, want %q", key, got[key], want)
		}
	}
}

func TestRedactSettingsMasksSecretQueryValues(t *testing.T) {
	got := RedactSettings(map[string]any{
		"whatsapp_webhook": "https://hooks.example/in?source=gowa&token=wh,tok#frag",
		"chatwoot_url":     "https://chat.example/?api_key=k1",
	})
	if want := "https://hooks.example/in?source=gowa&token=***#frag"; got["whatsapp_webhook"] != want {
		t.Fatalf("whatsapp_webhook = %#v, want %q", got["whatsapp_webhook"], want)
	}
	if want := "https://chat.example/?api_key=***"; got["chatwoot_url"] != want {
		t.Fatalf("chatwoot_url = %#v, want %q", got["chatwoot_url"], want)
	}
}

func TestRedactSettingsMasksPercentEncodedSecretQueryNames(t *testing.T) {
	got := RedactSettings(map[string]any{
		"whatsapp_webhook": "https://hooks.example/?to%6Ben=LIVE_TOKEN",
		"chatwoot_url":     "https://chat.example/?a%20b=1,https://hooks.example/in?p%61ssword=LIVE&x=1",
		"bad_escape_url":   "https://hooks.example/?token%zz=LIVE&ok=%zz",
	})
	for key, want := range map[string]string{
		"whatsapp_webhook": "https://hooks.example/?to%6Ben=***",
		"chatwoot_url":     "https://chat.example/?a%20b=1,https://hooks.example/in?p%61ssword=***&x=1",
		"bad_escape_url":   "https://hooks.example/?token%zz=***&ok=%zz",
	} {
		if got[key] != want {
			t.Fatalf("%s = %#v, want %q", key, got[key], want)
		}
	}
}

func TestRedactSettingsKeepsOAuthSettingsAndEmptySecrets(t *testing.T) {
	got := RedactSettings(map[string]any{
		"mcp_oauth_enabled":      true,
		"mcp_oauth_issuer_url":   "https://gowa.example",
		"mcp_oauth_resource_url": "https://gowa.example/mcp",
		"mcp_oauth_db_uri":       "postgres://oauth:oauthpass@db.internal:5432/oauth",
		"chatwoot_api_token":     "",
	})
	if got["mcp_oauth_enabled"] != true {
		t.Fatalf("mcp_oauth_enabled = %#v, want true", got["mcp_oauth_enabled"])
	}
	if got["mcp_oauth_issuer_url"] != "https://gowa.example" || got["mcp_oauth_resource_url"] != "https://gowa.example/mcp" {
		t.Fatalf("OAuth URLs were hidden: issuer=%#v resource=%#v", got["mcp_oauth_issuer_url"], got["mcp_oauth_resource_url"])
	}
	if got["mcp_oauth_db_uri"] != redactedSecret {
		t.Fatalf("mcp_oauth_db_uri = %#v, want %s", got["mcp_oauth_db_uri"], redactedSecret)
	}
	if got["chatwoot_api_token"] != "" {
		t.Fatalf("empty secret = %#v, want empty", got["chatwoot_api_token"])
	}
}

func TestRedactSettingsURLUserinfoOutsideSecretKeys(t *testing.T) {
	got := RedactSettings(map[string]any{
		"chatwoot_url": "https://agent:s3cret@chat.example",
	})
	gotURL, _ := got["chatwoot_url"].(string)
	if strings.Contains(gotURL, "s3cret") || strings.Contains(gotURL, "agent:s3cret") {
		t.Fatalf("userinfo leaked: %s", gotURL)
	}
	if !strings.Contains(gotURL, "https://***@chat.example") {
		t.Fatalf("redacted URL = %s", gotURL)
	}
}

func TestFormatProxyParseFailureOmitsUserinfo(t *testing.T) {
	raw := "socks5://user:pass@proxy.internal:not-a-port"
	_, err := url.Parse(raw)
	if err == nil {
		t.Fatal("expected url.Parse to fail")
	}
	if !strings.Contains(err.Error(), "user:pass@") {
		t.Fatalf("precondition: parser error should echo the URL, got %q", err.Error())
	}

	logged := fmt.Sprintf("failed to apply WHATSAPP_PROXY for device %s: %s", "device-1", FormatProxyParseFailure(raw))
	if strings.Contains(logged, "user:pass@") || strings.Contains(logged, err.Error()) {
		t.Fatalf("proxy parse log leaked credentials: %q", logged)
	}
	if !strings.Contains(logged, "scheme=socks5") || !strings.Contains(logged, "host=proxy.internal") || !strings.Contains(logged, "parse failure") {
		t.Fatalf("expected scheme, host, and parse failure, got %q", logged)
	}
}

func TestFormatProxyParseFailureKeepsHostWithoutEchoingError(t *testing.T) {
	raw := "ftp://user:pass@proxy.internal:21"
	_, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("expected parse to succeed so the failure is the unsupported scheme, got %v", err)
	}
	logged := FormatProxyParseFailure(raw)
	if strings.Contains(logged, "user:pass") {
		t.Fatalf("logged userinfo: %q", logged)
	}
	if !strings.Contains(logged, "scheme=ftp") || !strings.Contains(logged, "host=proxy.internal:21") {
		t.Fatalf("expected scheme and host, got %q", logged)
	}
}

func TestFormatProxyParseFailureCredentialOnlyURL(t *testing.T) {
	raw := "http://user:pass"
	logged := FormatProxyParseFailure(raw)
	if strings.Contains(logged, "user:pass") {
		t.Fatalf("logged credentials: %q", logged)
	}
	if !strings.Contains(logged, "scheme=http") || !strings.Contains(logged, "parse failure") {
		t.Fatalf("expected scheme and parse failure, got %q", logged)
	}
}

func TestFormatProxyParseFailureDropsNonNumericPort(t *testing.T) {
	for raw, want := range map[string]string{
		"ftp://example.com:supersecret":                   "scheme=ftp host=example.com: parse failure",
		"socks5://user:pass@[::1]:supersecret":            "scheme=socks5 host=[::1]: parse failure",
		"socks5://user:pass@[::1]:supersecret]:x":         "scheme=socks5: parse failure",
		"socks5://user:pass@proxy.example:supersecret]:x": "scheme=socks5: parse failure",
	} {
		if got := FormatProxyParseFailure(raw); got != want {
			t.Fatalf("FormatProxyParseFailure(%q) = %q, want %q", raw, got, want)
		}
	}
}
