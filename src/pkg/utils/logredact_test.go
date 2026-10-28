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
		"chatwoot_webhook_url":                 "https://public.example/chatwoot/webhook",
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
	if got["chatwoot_webhook_url"] != "https://public.example/chatwoot/webhook" {
		t.Fatalf("chatwoot webhook URL should stay visible, got %#v", got["chatwoot_webhook_url"])
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
