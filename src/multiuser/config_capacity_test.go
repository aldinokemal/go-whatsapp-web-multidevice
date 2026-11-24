package multiuser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func capEnv(extra map[string]string) func(string) string {
	m := map[string]string{"WTF_MULTIUSER": "true", "SESSION_SECRET": "0123456789abcdef0123456789abcdef"}
	for k, v := range extra {
		m[k] = v
	}
	return env(m)
}

func TestCapacityDefaults(t *testing.T) {
	s, err := Load(capEnv(nil))
	require.NoError(t, err)
	require.Equal(t, 0, s.MaxPhones, "no cap unless set")
	require.Equal(t, 2, s.DefaultMaxDevices, "today's column default")
}

func TestCapacityParsed(t *testing.T) {
	s, err := Load(capEnv(map[string]string{"WTF_MAX_PHONES": " 20 ", "WTF_DEFAULT_MAX_DEVICES": "3"}))
	require.NoError(t, err)
	require.Equal(t, 20, s.MaxPhones)
	require.Equal(t, 3, s.DefaultMaxDevices)
}

func TestCapacityRejectsBadValues(t *testing.T) {
	for name, e := range map[string]map[string]string{
		"phones not a number":  {"WTF_MAX_PHONES": "lots"},
		"phones negative":      {"WTF_MAX_PHONES": "-1"},
		"devices zero":         {"WTF_DEFAULT_MAX_DEVICES": "0"},
		"devices over the max": {"WTF_DEFAULT_MAX_DEVICES": "21"},
		"devices not a number": {"WTF_DEFAULT_MAX_DEVICES": "two"},
		"open sign-up, no cap": {"WTF_SIGNUP_OPEN": "true"},
		"open sign-up, cap 0":  {"WTF_SIGNUP_OPEN": "true", "WTF_MAX_PHONES": "0"},
	} {
		_, err := Load(capEnv(e))
		require.Error(t, err, name)
	}
	_, err := Load(capEnv(map[string]string{"WTF_SIGNUP_OPEN": "true", "WTF_MAX_PHONES": "5"}))
	require.NoError(t, err, "open sign-up with a cap is fine")
}
