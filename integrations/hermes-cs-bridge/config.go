package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting of the bridge. All values come from environment
// variables (see .env.example).
type Config struct {
	ListenAddr    string
	WebhookSecret string

	GowaURL      string
	GowaUser     string
	GowaPass     string
	GowaDeviceID string

	HermesURL       string
	HermesAPIKey    string
	HermesModel     string
	HermesTimeout   time.Duration
	SessionPrefix   string
	SystemPrompt    string
	MaxMessageChars int

	AdminNumbers []string

	PauseDuration   time.Duration
	Debounce        time.Duration
	DebounceMaxWait time.Duration
	RateLimitPerHr  int

	FallbackMessage  string
	HandoffMessage   string
	RateLimitMessage string

	StateFile    string
	SettingsFile string
	MediaDir     string
	Typing       bool

	// AdminToken protects the /cs-ai settings page. GOWA's proxy sends it after
	// Basic Auth succeeded; when empty the settings page is disabled.
	AdminToken string

	// Dashboard CRM: lead events go to DashboardURL + /integrations/cs-ai/events.
	// Off when the URL or the token is empty.
	DashboardURL       string
	DashboardToken     string
	DashboardOutboxDir string
}

func loadConfig() (Config, error) {
	c := Config{
		ListenAddr:         env("LISTEN_ADDR", "127.0.0.1:8650"),
		WebhookSecret:      os.Getenv("WEBHOOK_SECRET"),
		GowaURL:            strings.TrimRight(env("GOWA_URL", "http://127.0.0.1:3000"), "/"),
		GowaUser:           os.Getenv("GOWA_USER"),
		GowaPass:           os.Getenv("GOWA_PASS"),
		GowaDeviceID:       os.Getenv("GOWA_DEVICE_ID"),
		HermesURL:          strings.TrimRight(env("HERMES_URL", "http://127.0.0.1:8642"), "/"),
		HermesAPIKey:       os.Getenv("HERMES_API_KEY"),
		HermesModel:        os.Getenv("HERMES_MODEL"),
		SessionPrefix:      env("HERMES_SESSION_PREFIX", "wa-cs-"),
		StateFile:          env("STATE_FILE", "/data/state.json"),
		SettingsFile:       env("SETTINGS_FILE", "/data/settings.json"),
		MediaDir:           env("MEDIA_DIR", "/data/media"),
		AdminToken:         os.Getenv("ADMIN_TOKEN"),
		DashboardURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("DASHBOARD_URL")), "/"),
		DashboardToken:     strings.TrimSpace(os.Getenv("DASHBOARD_INGEST_TOKEN")),
		DashboardOutboxDir: env("DASHBOARD_OUTBOX_DIR", "/data/outbox-dashboard"),
		// No technical wording towards leads (Ruang Sanad CS rules).
		FallbackMessage: env("FALLBACK_MESSAGE", "Terima kasih, Kak. Pesan Kakak sudah kami terima. Admin akan membalas di chat ini secepatnya."),
		HandoffMessage:  env("HANDOFF_MESSAGE", "Baik, Kak. Agar informasinya tepat, saya teruskan ke admin, ya. Admin akan membalas di chat ini."),
		// Empty by default: when the hourly limit is hit the admin is notified
		// instead of sending the lead a "too many messages" notice.
		RateLimitMessage: os.Getenv("RATE_LIMIT_MESSAGE"),
	}

	var err error
	if c.HermesTimeout, err = envDuration("HERMES_TIMEOUT", 180*time.Second); err != nil {
		return c, err
	}
	if c.PauseDuration, err = envDuration("PAUSE_DURATION", 6*time.Hour); err != nil {
		return c, err
	}
	if c.Debounce, err = envDuration("DEBOUNCE", 4*time.Second); err != nil {
		return c, err
	}
	if c.DebounceMaxWait, err = envDuration("DEBOUNCE_MAX_WAIT", 15*time.Second); err != nil {
		return c, err
	}
	if c.RateLimitPerHr, err = envInt("RATE_LIMIT_PER_HOUR", 30); err != nil {
		return c, err
	}
	if c.MaxMessageChars, err = envInt("MAX_MESSAGE_CHARS", 3000); err != nil {
		return c, err
	}
	c.Typing = env("TYPING_INDICATOR", "true") != "false"

	for _, n := range strings.Split(os.Getenv("ADMIN_NUMBERS"), ",") {
		if d := normalizePhone(n); d != "" {
			c.AdminNumbers = append(c.AdminNumbers, d)
		}
	}

	c.SystemPrompt = defaultSystemPrompt
	if path := os.Getenv("CS_SYSTEM_PROMPT_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read CS_SYSTEM_PROMPT_FILE: %w", err)
		}
		c.SystemPrompt = string(b)
	}

	var missing []string
	for _, kv := range [][2]string{
		{"WEBHOOK_SECRET", c.WebhookSecret}, {"GOWA_USER", c.GowaUser}, {"GOWA_PASS", c.GowaPass},
		{"GOWA_DEVICE_ID", c.GowaDeviceID}, {"HERMES_API_KEY", c.HermesAPIKey},
	} {
		if kv[1] == "" {
			missing = append(missing, kv[0])
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	if len(c.WebhookSecret) < 16 {
		return c, errors.New("WEBHOOK_SECRET must be at least 16 characters")
	}
	if c.AdminToken != "" && len(c.AdminToken) < 24 {
		return c, errors.New("ADMIN_TOKEN must be at least 24 characters (openssl rand -hex 32)")
	}
	if c.DashboardToken != "" && len(c.DashboardToken) < 32 {
		return c, errors.New("DASHBOARD_INGEST_TOKEN must be at least 32 characters (openssl rand -hex 32)")
	}
	if c.DashboardURL != "" && !strings.HasPrefix(c.DashboardURL, "http://") && !strings.HasPrefix(c.DashboardURL, "https://") {
		return c, errors.New("DASHBOARD_URL must start with http:// or https://")
	}
	return c, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w (use e.g. 30s, 5m, 6h)", key, err)
	}
	return d, nil
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

// normalizePhone turns "+62 812-3456", "0812..." or "62812...@s.whatsapp.net"
// into plain international digits ("62812..."). Returns "" when no digits.
func normalizePhone(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "@:"); i >= 0 {
		s = s[:i]
	}
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if strings.HasPrefix(d, "0") {
		d = "62" + d[1:]
	}
	return d
}
