// Package multiuser is WTF's multi-user layer over GOWA. Everything here is
// inert unless WTF_MULTIUSER=true.
package multiuser

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
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
	// MCPEnabled turns on /mcp with per-user OAuth (WTF_MCP_ENABLED, default
	// true in multi-user mode).
	MCPEnabled bool
	// MCPClientHosts are the exact hosts whose client ID metadata documents
	// are accepted (WTF_MCP_CLIENT_HOSTS, default claude.ai: Claude only).
	MCPClientHosts []string
	// CIMDHTTPClient fetches client ID metadata documents. Nil (always, in
	// production; no setting reaches it) means the netguard client. Tests set
	// it to reach a local document server, which netguard rightly refuses.
	CIMDHTTPClient *http.Client
}

// MCPResourceURL is the MCP endpoint's public address. The OAuth issuer,
// the protected-resource identifier and the connect link all derive from it.
func (s Settings) MCPResourceURL() string { return s.PublicURL + "/mcp" }

// hostnameRe is an exact DNS name: labels of letters, digits and hyphens.
var hostnameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

func parseClientHosts(raw string) ([]string, error) {
	var out []string
	for _, h := range strings.Split(raw, ",") {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if !hostnameRe.MatchString(h) {
			return nil, errors.New("WTF_MCP_CLIENT_HOSTS takes exact host names separated by commas (no scheme, port, path or wildcard)")
		}
		out = append(out, h)
	}
	if len(out) == 0 {
		out = []string{"claude.ai"}
	}
	return out, nil
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

	s.MCPEnabled = true
	if v := strings.TrimSpace(getenv("WTF_MCP_ENABLED")); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return s, errors.New("WTF_MCP_ENABLED must be true or false")
		}
		s.MCPEnabled = on
	}
	hosts, err := parseClientHosts(getenv("WTF_MCP_CLIENT_HOSTS"))
	if err != nil {
		return s, err
	}
	s.MCPClientHosts = hosts

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
// share one server. MCP stays off here; ApplyMCP, called after it once the
// per-user OAuth wiring is in place, is the only thing that turns it back on.
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
	installOutboundGuard()
}

// ApplyMCP sets upstream's MCP flags from s: MCP and its OAuth server go on or
// off together, and issuer and resource derive from the public URL. It clears
// Basic Auth again, so the Basic fallback on /mcp can never be reached in
// multi-user mode whatever ran before it.
//
// OAuth needs a public https issuer, so with a plain-http public URL (local
// development) MCP stays off, with a warning. The returned settings say what
// was applied; pass them on to Setup.
func ApplyMCP(s Settings) Settings {
	if s.MCPEnabled && !strings.HasPrefix(s.PublicURL, "https://") {
		logrus.Warn("[WTF] MCP is off: it needs an https WTF_PUBLIC_URL")
		s.MCPEnabled = false
	}
	config.AppBasicAuthCredential = nil
	config.McpEnabled = s.MCPEnabled
	config.McpOAuthEnabled = s.MCPEnabled
	config.McpOAuthIssuerURL = s.PublicURL
	config.McpOAuthResourceURL = s.MCPResourceURL()
	return s
}
