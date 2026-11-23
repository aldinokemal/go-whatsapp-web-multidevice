// Package multiuser is WTF's multi-user layer over GOWA. Everything here is
// inert unless WTF_MULTIUSER=true.
package multiuser

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/sirupsen/logrus"
)

type Settings struct {
	Enabled     bool
	DBURI       string
	ProductName string
	PublicURL   string
	// PublicURLIsFallback is true when neither WTF_PUBLIC_URL nor RENDER_EXTERNAL_URL was set.
	PublicURLIsFallback bool
	SessionSecret       []byte
	Superadmins         map[string]bool
	SignupOpen          bool
}

// Load reads WTF_* settings. getenv is os.Getenv in production and a map in tests.
func Load(getenv func(string) string) (Settings, error) {
	s := Settings{
		Enabled:     strings.EqualFold(strings.TrimSpace(getenv("WTF_MULTIUSER")), "true"),
		DBURI:       "file:storages/accounts.db",
		ProductName: "WTF",
		Superadmins: map[string]bool{},
		SignupOpen:  strings.EqualFold(strings.TrimSpace(getenv("WTF_SIGNUP_OPEN")), "true"),
	}
	if !s.Enabled {
		return s, nil
	}
	if v := strings.TrimSpace(getenv("WTF_DB_URI")); v != "" {
		s.DBURI = v
	}
	if v := strings.TrimSpace(getenv("WTF_PRODUCT_NAME")); v != "" {
		s.ProductName = v
	}
	s.PublicURL = strings.TrimSpace(getenv("WTF_PUBLIC_URL"))
	if s.PublicURL == "" {
		s.PublicURL = strings.TrimSpace(getenv("RENDER_EXTERNAL_URL"))
	}
	if s.PublicURL == "" {
		s.PublicURL = "http://localhost:3000"
		s.PublicURLIsFallback = true
	}
	s.PublicURL = strings.TrimRight(s.PublicURL, "/")

	secret := getenv("SESSION_SECRET")
	if len(secret) < 32 {
		return s, errors.New("SESSION_SECRET must be at least 32 characters when WTF_MULTIUSER=true")
	}
	s.SessionSecret = []byte(secret)

	for _, raw := range strings.Split(getenv("WTF_SUPERADMIN_PHONES"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		phone, err := store.NormalizePhone(raw)
		if err != nil {
			return s, errors.New("WTF_SUPERADMIN_PHONES holds an invalid number")
		}
		s.Superadmins[phone] = true
	}
	return s, nil
}

func IsSuperadmin(s Settings, phone string) bool { return s.Superadmins[phone] }

// Apply turns off every upstream feature that is unsafe when several people
// share one server. Phase 2 re-enables MCP with per-user OAuth.
func Apply() {
	if len(config.AppBasicAuthCredential) > 0 {
		logrus.Warn("[WTF] APP_BASIC_AUTH is ignored in multi-user mode")
	}
	config.AppBasicAuthCredential = nil
	config.McpEnabled = false
	config.McpOAuthEnabled = false
	config.ChatwootEnabled = false
	config.WhatsappWebhook = nil
	config.WhatsappAutoReplyMessage = ""
	config.AppUIEnabled = false
	config.WhatsappAutoDownloadMedia = false
}
