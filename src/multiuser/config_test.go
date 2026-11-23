package multiuser

import (
	"testing"
	"time"

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

func TestLoadFlagsLocalhostFallback(t *testing.T) {
	env := map[string]string{"WTF_MULTIUSER": "true", "SESSION_SECRET": "0123456789abcdef0123456789abcdef"}
	s, err := Load(func(k string) string { return env[k] })
	require.NoError(t, err)
	require.True(t, s.PublicURLIsFallback)
	env["WTF_PUBLIC_URL"] = "https://wtf.example"
	s, _ = Load(func(k string) string { return env[k] })
	require.False(t, s.PublicURLIsFallback)
}

const testSecret = "0123456789abcdef0123456789abcdef"

func muEnv(extra map[string]string) func(string) string {
	m := map[string]string{"WTF_MULTIUSER": "true", "SESSION_SECRET": testSecret}
	for k, v := range extra {
		m[k] = v
	}
	return env(m)
}

// Behaviour: MCP is on by default once multi-user mode is on, for Claude only.
func TestMCPDefaultsOnInMultiUser(t *testing.T) {
	s, err := Load(muEnv(nil))
	require.NoError(t, err)
	require.True(t, s.MCPEnabled)
	require.Equal(t, []string{"claude.ai"}, s.MCPClientHosts)

	off, err := Load(env(nil))
	require.NoError(t, err)
	require.False(t, off.MCPEnabled, "single-user mode never turns this on")
}

func TestMCPCanBeTurnedOff(t *testing.T) {
	s, err := Load(muEnv(map[string]string{"WTF_MCP_ENABLED": "false"}))
	require.NoError(t, err)
	require.False(t, s.MCPEnabled)

	_, err = Load(muEnv(map[string]string{"WTF_MCP_ENABLED": "sometimes"}))
	require.Error(t, err, "an unreadable switch fails loudly rather than guessing")
}

func TestMCPClientHostsParsed(t *testing.T) {
	s, err := Load(muEnv(map[string]string{"WTF_MCP_CLIENT_HOSTS": " Claude.AI , chatgpt.com ,"}))
	require.NoError(t, err)
	require.Equal(t, []string{"claude.ai", "chatgpt.com"}, s.MCPClientHosts)
}

// Security: the client-host allowlist names exact hosts; anything that could
// widen it (a wildcard) or be misread (a scheme, a port, a path) refuses to load.
func TestMCPClientHostsRefusesWildcardsAndSchemes(t *testing.T) {
	for _, bad := range []string{"*.claude.ai", "https://claude.ai", "claude.ai:443", "claude.ai/x", "claude ai", "user@claude.ai"} {
		_, err := Load(muEnv(map[string]string{"WTF_MCP_CLIENT_HOSTS": bad}))
		require.Error(t, err, bad)
	}
}

func TestMCPResourceURLDerivesFromPublicURL(t *testing.T) {
	s, err := Load(muEnv(map[string]string{"WTF_PUBLIC_URL": "https://wtf.example.com/"}))
	require.NoError(t, err)
	require.Equal(t, "https://wtf.example.com/mcp", s.MCPResourceURL())
}

func TestApplyMCPNeverEnablesBasicAuth(t *testing.T) {
	saved := []any{config.McpEnabled, config.McpOAuthEnabled, config.McpOAuthIssuerURL, config.McpOAuthResourceURL, config.AppBasicAuthCredential}
	t.Cleanup(func() {
		config.McpEnabled = saved[0].(bool)
		config.McpOAuthEnabled = saved[1].(bool)
		config.McpOAuthIssuerURL = saved[2].(string)
		config.McpOAuthResourceURL = saved[3].(string)
		config.AppBasicAuthCredential = saved[4].([]string)
	})
	config.AppBasicAuthCredential = []string{"a:b"}
	s, err := Load(muEnv(map[string]string{"WTF_PUBLIC_URL": "https://wtf.example.com"}))
	require.NoError(t, err)
	Apply()
	ApplyMCP(s)
	require.True(t, config.McpEnabled)
	require.True(t, config.McpOAuthEnabled)
	require.Equal(t, "https://wtf.example.com", config.McpOAuthIssuerURL)
	require.Equal(t, "https://wtf.example.com/mcp", config.McpOAuthResourceURL)
	require.Empty(t, config.AppBasicAuthCredential)

	config.AppBasicAuthCredential = []string{"a:b"}
	s.MCPEnabled = false
	ApplyMCP(s)
	require.False(t, config.McpEnabled)
	require.False(t, config.McpOAuthEnabled)
	require.Empty(t, config.AppBasicAuthCredential, "ApplyMCP clears Basic Auth too, whatever ran before it")
}

func TestApplyMCPNeedsAnHTTPSPublicURL(t *testing.T) {
	saved := []any{config.McpEnabled, config.McpOAuthEnabled, config.McpOAuthIssuerURL, config.McpOAuthResourceURL}
	t.Cleanup(func() {
		config.McpEnabled = saved[0].(bool)
		config.McpOAuthEnabled = saved[1].(bool)
		config.McpOAuthIssuerURL = saved[2].(string)
		config.McpOAuthResourceURL = saved[3].(string)
	})
	s, err := Load(muEnv(map[string]string{})) // falls back to http://localhost:3000
	require.NoError(t, err)
	require.True(t, s.MCPEnabled)
	applied := ApplyMCP(s)
	require.False(t, applied.MCPEnabled, "OAuth needs an https issuer; local http development runs without MCP")
	require.False(t, config.McpEnabled)
	require.False(t, config.McpOAuthEnabled)
}

// Behaviour: the retention window. A purge only ever runs on an explicit,
// positive window; anything unreadable refuses to start rather than guessing.
func TestRetentionHoursParsed(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
		hours   int
		on      bool
	}{
		{"default is 24", nil, false, 24, true},
		{"explicit 48", map[string]string{"WTF_RETENTION_HOURS": " 48 "}, false, 48, true},
		{"one hour is the minimum", map[string]string{"WTF_RETENTION_HOURS": "1"}, false, 1, true},
		{"zero is off with sign-up closed", map[string]string{"WTF_RETENTION_HOURS": "0"}, false, 0, false},
		{"zero is refused with sign-up open", map[string]string{"WTF_RETENTION_HOURS": "0", "WTF_SIGNUP_OPEN": "true"}, true, 0, false},
		{"negative is refused", map[string]string{"WTF_RETENTION_HOURS": "-1"}, true, 0, false},
		{"text is refused", map[string]string{"WTF_RETENTION_HOURS": "abc"}, true, 0, false},
		{"a fraction is refused", map[string]string{"WTF_RETENTION_HOURS": "1.5"}, true, 0, false},
		{"too large to be a duration is refused", map[string]string{"WTF_RETENTION_HOURS": "9999999999"}, true, 0, false},
		{"a duration string is refused", map[string]string{"WTF_RETENTION_HOURS": "24h"}, true, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Load(muEnv(tc.env))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.hours, s.RetentionHours)
			w, on := s.RetentionWindow()
			require.Equal(t, tc.on, on)
			if on {
				require.Equal(t, time.Duration(tc.hours)*time.Hour, w)
			} else {
				require.Zero(t, w)
			}
		})
	}
}

// Behaviour: single-user mode never has a retention window, whatever is set.
func TestRetentionOffOutsideMultiUser(t *testing.T) {
	s, err := Load(env(map[string]string{"WTF_RETENTION_HOURS": "24"}))
	require.NoError(t, err)
	_, on := s.RetentionWindow()
	require.False(t, on)
}
