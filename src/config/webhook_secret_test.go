package config

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func digest(body, key []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookHMACIsNotThePublishedDefault(t *testing.T) {
	prev := WhatsappWebhookSecret
	t.Cleanup(func() { WhatsappWebhookSecret = prev })

	for _, configured := range []string{"", "secret", " secret "} {
		WhatsappWebhookSecret = configured
		replaced, err := UseEphemeralWebhookSecret()
		require.NoError(t, err)
		require.True(t, replaced)
		require.False(t, WebhookSecretUnusable(WhatsappWebhookSecret))
		require.NotEqual(t, PublishedWhatsappWebhookSecret, WhatsappWebhookSecret)

		body := []byte(`{"event":"message","text":"hello"}`)
		got := digest(body, []byte(WhatsappWebhookSecret))
		published := digest(body, []byte(PublishedWhatsappWebhookSecret))
		require.NotEqual(t, published, got, "configured %q still signs with the published default", configured)
	}
}

func TestUseEphemeralWebhookSecretKeepsConfiguredKey(t *testing.T) {
	prev := WhatsappWebhookSecret
	t.Cleanup(func() { WhatsappWebhookSecret = prev })

	WhatsappWebhookSecret = "operator-secret"
	replaced, err := UseEphemeralWebhookSecret()
	require.NoError(t, err)
	require.False(t, replaced)
	require.Equal(t, "operator-secret", WhatsappWebhookSecret)
}
