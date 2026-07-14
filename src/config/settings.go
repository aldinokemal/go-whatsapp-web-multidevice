package config

import (
	"time"

	"go.mau.fi/whatsmeow/proto/waCompanionReg"
)

var (
	AppVersion             = "v8.11.0"
	AppPort                = "3000"
	AppHost                = "0.0.0.0"
	AppDebug               = false
	AppOs                  = "GOWA"
	AppPlatform            = waCompanionReg.DeviceProps_PlatformType(1)
	AppBasicAuthCredential []string
	AppBasePath            = ""
	AppTrustedProxies      []string // Trusted proxy IP ranges (e.g., "0.0.0.0/0" for all, or specific CIDRs)
	AppCORSAllowedOrigins  []string // CORS allowed origins; empty means "*" (any origin)

	// Web UI (gowa-ui) runtime download settings. The dashboard is a separate
	// project released as a single HTML file; gowa fetches the latest release
	// asset and serves it at "/".
	AppUIEnabled        = true
	AppUIAutoUpdate     = true
	AppUIRepo           = "aldinokemal/gowa-ui"
	AppUIAssetName      = "gowa-ui.html"
	AppUIUpdateInterval = 3 * time.Hour
	AppUIGithubToken    = "" // optional, raises the GitHub API rate limit
	AppUIAssetSHA256    = "" // optional supply-chain pin: only serve the asset with this sha256

	McpPort = "8080"
	McpHost = "localhost"

	PathQrCode    = "statics/qrcode"
	PathSendItems = "statics/senditems"
	PathMedia     = "statics/media"
	PathStorages  = "storages"
	PathUICache   = "storages/ui"

	DBURI     = "file:storages/whatsapp.db"
	DBKeysURI = ""

	WhatsappAutoReplyMessage          string
	WhatsappAutoMarkRead              = false // Auto-mark incoming messages as read
	WhatsappAutoDownloadMedia         = true  // Auto-download media from incoming messages
	WhatsappWebhook                   []string
	WhatsappWebhookSecret             = "secret"
	WhatsappWebhookInsecureSkipVerify = false          // Skip TLS certificate verification for webhooks (insecure)
	WhatsappWebhookEvents             []string         // Whitelist of events to forward to webhook (empty = all events)
	WhatsappWebhookIgnoreJids         []string         // JIDs (or "@g.us"/"@s.whatsapp.net"/"@lid" wildcards) to skip when forwarding to webhooks
	WhatsappAutoRejectCall                     = false // Auto-reject incoming calls
	WhatsappLogLevel                           = "ERROR"
	WhatsappSettingMaxImageSize       int64    = 20000000  // 20MB
	WhatsappSettingMaxFileSize        int64    = 50000000  // 50MB
	WhatsappSettingMaxVideoSize       int64    = 100000000 // 100MB
	WhatsappSettingMaxDownloadSize    int64    = 500000000 // 500MB
	WhatsappTypeUser                           = "@s.whatsapp.net"
	WhatsappTypeGroup                          = "@g.us"
	WhatsappTypeLid                            = "@lid"
	WhatsappTypeNewsletter                     = "@newsletter"
	WhatsappAccountValidation                  = true
	WhatsappPresenceOnConnect                  = "unavailable" // Presence to send on connect: "available", "unavailable", or "none"
	WhatsappPresencePulseEnabled               = true          // Periodically pulse presence available, then unavailable
	WhatsappPresencePulseInterval              = 24 * time.Hour
	WhatsappPresencePulseDuration              = 5 * time.Minute

	// WhatsappProxy is forwarded to whatsmeow's *Client.SetProxyAddress before
	// Connect. Accepts SOCKS5/HTTP/HTTPS schemes, e.g.
	// "socks5://user:pass@host:1080" or "http://host:8080". Empty = direct
	// (no proxy). Useful for self-hosted deployments behind DPI / network
	// egress restrictions where standard HTTP_PROXY env vars do not apply
	// to the WhatsApp WebSocket dialer.
	WhatsappProxy = ""

	ChatStorageURI               = "file:storages/chatstorage.db"
	ChatStorageEnableForeignKeys = true
	ChatStorageEnableWAL         = true
	ChatStorageMaxOpenConns      = 5 // Max concurrent SQLite connections for chat storage (WAL allows concurrent readers + 1 writer)

	ChatwootEnabled   = false
	ChatwootURL       = ""
	ChatwootAPIToken  = ""
	ChatwootAccountID = 0
	ChatwootInboxID   = 0
	ChatwootDeviceID  = "" // Device ID for outbound messages (required for multi-device)

	// Chatwoot History Sync settings
	ChatwootImportMessages          = false // Enable message history import to Chatwoot
	ChatwootDaysLimitImportMessages = 3     // Days of history to import (default: 3)

	// ChatwootImportDBURI, when set, enables the direct-Postgres import path.
	// Historical sync will INSERT directly into
	// Chatwoot's schema instead of using the public REST API, which preserves
	// original WhatsApp timestamps, senders, and group subjects.
	//
	// Format: postgresql://user:pass@host:5432/chatwoot_production[?sslmode=disable]
	// When empty, the REST path is used (unchanged behavior). Live message
	// forwarding and inbound handling always use REST, regardless of this flag.
	ChatwootImportDBURI = ""

	// ChatwootImportPlaceholderMediaMessage controls what is inserted as the
	// message body for media messages when the importer could not download
	// the media file (e.g., URL expired). When true, inserts a localized
	// placeholder; when false, leaves the body empty.
	ChatwootImportPlaceholderMediaMessage = true
	// ChatwootImportMediaWithREST sends media history rows through Chatwoot's
	// REST attachment endpoint while direct-DB import handles non-media rows.
	// This preserves attachments at the cost of Chatwoot assigning media-row
	// timestamps at upload time.
	ChatwootImportMediaWithREST = false

	// Chatwoot auto-provisioning. When ChatwootAutoCreate
	// is true, the integration creates — or reuses, matched by name — an
	// API-channel inbox on startup and resolves ChatwootInboxID automatically,
	// so operators only need URL + token + account id. ChatwootWebhookURL is the
	// publicly reachable URL of this app's /chatwoot/webhook endpoint; when set
	// it is registered on the inbox so Chatwoot agent replies reach WhatsApp.
	ChatwootAutoCreate = false
	ChatwootInboxName  = "WhatsApp"
	ChatwootWebhookURL = ""
	// Optional shared secret for incoming Chatwoot webhooks. When empty, inbound
	// webhook requests remain unauthenticated for backward compatibility.
	ChatwootWebhookSecret = ""

	// ChatwootAllowedHosts optionally restricts which Chatwoot hosts a
	// per-device config may point at. When non-empty, a config's chatwoot_url
	// host must match one of these entries (exact, case-insensitive). It hardens
	// the SSRF surface introduced by operator-supplied per-device URLs in
	// deployments where authenticated API users are not fully trusted.
	ChatwootAllowedHosts []string

	// Chatwoot conversation handling. ChatwootReopenConversation reuses (and
	// reopens) a resolved conversation for a returning contact instead of
	// opening a new one; ChatwootConversationPending opens freshly-created
	// conversations in "pending" rather than "open" so they land in the agent's
	// unassigned queue. Both settings keep the REST and direct-DB paths
	// consistent.
	ChatwootReopenConversation  = true
	ChatwootConversationPending = false

	// ChatwootIgnoreJids lists WhatsApp JIDs — or the wildcards "@g.us" /
	// "@s.whatsapp.net" — that must never be mirrored to Chatwoot, on top of the
	// always-ignored system JIDs (status@broadcast, 0@s.whatsapp.net).
	ChatwootIgnoreJids []string

	// Chatwoot outbound signature. When ChatwootSignMsg is true, agent replies
	// sent from Chatwoot are prefixed with the agent's name (joined to the body
	// by ChatwootSignDelimiter) before delivery to WhatsApp.
	ChatwootSignMsg       = false
	ChatwootSignDelimiter = "\n\n"

	// Chatwoot edit/delete propagation. When enabled, WhatsApp message edits and
	// delete-for-everyone events are mirrored into the Chatwoot conversation as
	// threaded notes referencing the original message.
	ChatwootForwardEdits   = true
	ChatwootForwardDeletes = true

	// Chatwoot Evolution-compatible state propagation. Read sync updates
	// Chatwoot last-seen from WhatsApp receipts and marks WhatsApp messages read
	// after agent replies. Delete sync removes the linked Chatwoot/WhatsApp
	// message when the opposite side reports deletion.
	ChatwootMessageRead   = false
	ChatwootMessageDelete = false
)
