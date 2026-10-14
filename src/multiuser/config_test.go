package multiuser

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadDisabledByDefault(t *testing.T) {
	s, err := Load(env(nil))
	require.NoError(t, err)
	require.False(t, s.Enabled)
}

func TestLoadRequiresStrongSecret(t *testing.T) {
	_, err := Load(env(map[string]string{"WTF_MULTIUSER": "true", "SESSION_SECRET": "short"}))
	require.Error(t, err)
}

func TestLoadPublicURLFallsBackToRender(t *testing.T) {
	s, err := Load(env(map[string]string{
		"WTF_MULTIUSER":         "true",
		"SESSION_SECRET":        "0123456789abcdef0123456789abcdef",
		"RENDER_EXTERNAL_URL":   "https://wtf.onrender.com/",
		"WTF_SUPERADMIN_PHONES": "+27 82 555 0100, 15550001111",
	}))
	require.NoError(t, err)
	require.Equal(t, "https://wtf.onrender.com", s.PublicURL)
	require.Equal(t, "WTF", s.ProductName)
	require.False(t, s.SignupOpen)
	require.True(t, IsSuperadmin(s, "+27825550100"))
	require.True(t, IsSuperadmin(s, "+15550001111"))
	require.False(t, IsSuperadmin(s, "+15550002222"))
}

func TestApplyForcesSharedFeaturesOff(t *testing.T) {
	saved := []any{config.McpEnabled, config.ChatwootEnabled, config.WhatsappWebhook, config.AppUIEnabled, config.AppBasicAuthCredential}
	t.Cleanup(func() {
		config.McpEnabled = saved[0].(bool)
		config.ChatwootEnabled = saved[1].(bool)
		config.WhatsappWebhook = saved[2].([]string)
		config.AppUIEnabled = saved[3].(bool)
		config.AppBasicAuthCredential = saved[4].([]string)
	})
	config.McpEnabled, config.ChatwootEnabled, config.AppUIEnabled = true, true, true
	config.WhatsappWebhook = []string{"https://hook"}
	config.AppBasicAuthCredential = []string{"a:b"}
	Apply()
	require.False(t, config.McpEnabled)
	require.False(t, config.ChatwootEnabled)
	require.False(t, config.AppUIEnabled)
	require.Empty(t, config.WhatsappWebhook)
	require.Empty(t, config.AppBasicAuthCredential)
}
