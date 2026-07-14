package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.mau.fi/whatsmeow"
)

var (
	// Whatsapp
	whatsappCli *whatsmeow.Client

	// Chat Storage
	chatStorageDB   *sql.DB
	chatStorageRepo domainChatStorage.IChatStorageRepository

	// Usecase
	appUsecase        domainApp.IAppUsecase
	callUsecase       domainCall.ICallUsecase
	chatUsecase       domainChat.IChatUsecase
	sendUsecase       domainSend.ISendUsecase
	userUsecase       domainUser.IUserUsecase
	messageUsecase    domainMessage.IMessageUsecase
	groupUsecase      domainGroup.IGroupUsecase
	newsletterUsecase domainNewsletter.INewsletterUsecase
	deviceUsecase     domainDevice.IDeviceUsecase
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Short: "Send free whatsapp API",
	Long: `This application is from clone https://github.com/aldinokemal/go-whatsapp-web-multidevice, 
you can send whatsapp over http api but your whatsapp account have to be multi device version`,
}

func init() {
	// Load environment variables first
	utils.LoadConfig(".")

	time.Local = time.UTC

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Initialize flags first, before any subcommands are added
	initFlags()

	// Then initialize other components
	cobra.OnInitialize(initEnvConfig, initApp)
}

// initEnvConfig loads configuration from environment variables
func initEnvConfig() {
	fmt.Println(viper.AllSettings())
	// Application settings
	if envPort := viper.GetString("app_port"); envPort != "" {
		config.AppPort = envPort
	}
	if envHost := viper.GetString("app_host"); envHost != "" {
		config.AppHost = envHost
	}
	if envDebug := viper.GetBool("app_debug"); envDebug {
		config.AppDebug = envDebug
	}
	if envOs := viper.GetString("app_os"); envOs != "" {
		config.AppOs = envOs
	}
	if envBasicAuth := viper.GetString("app_basic_auth"); envBasicAuth != "" {
		credential := strings.Split(envBasicAuth, ",")
		config.AppBasicAuthCredential = credential
	}
	if envBasePath := viper.GetString("app_base_path"); envBasePath != "" {
		config.AppBasePath = envBasePath
	}
	if envTrustedProxies := viper.GetString("app_trusted_proxies"); envTrustedProxies != "" {
		proxies := strings.Split(envTrustedProxies, ",")
		config.AppTrustedProxies = proxies
	}
	if envCORSOrigins := viper.GetString("app_cors_allowed_origins"); envCORSOrigins != "" {
		config.AppCORSAllowedOrigins = strings.Split(envCORSOrigins, ",")
	}

	// Web UI settings. Guard on GetString != "" rather than viper.IsSet:
	// IsSet does not consult AutomaticEnv, so plain environment variables
	// (e.g. in Docker) would be ignored for bool/duration keys.
	if viper.GetString("app_ui_enabled") != "" {
		config.AppUIEnabled = viper.GetBool("app_ui_enabled")
	}
	if viper.GetString("app_ui_auto_update") != "" {
		config.AppUIAutoUpdate = viper.GetBool("app_ui_auto_update")
	}
	if envUIRepo := viper.GetString("app_ui_repo"); envUIRepo != "" {
		config.AppUIRepo = envUIRepo
	}
	if envUIAsset := viper.GetString("app_ui_asset_name"); envUIAsset != "" {
		config.AppUIAssetName = envUIAsset
	}
	if viper.GetString("app_ui_update_interval") != "" {
		if interval := viper.GetDuration("app_ui_update_interval"); interval > 0 {
			config.AppUIUpdateInterval = interval
		}
	}
	if envUIToken := viper.GetString("app_ui_github_token"); envUIToken != "" {
		config.AppUIGithubToken = envUIToken
	}
	if envUIPin := viper.GetString("app_ui_asset_sha256"); envUIPin != "" {
		config.AppUIAssetSHA256 = envUIPin
	}

	// Database settings
	if envDBURI := viper.GetString("db_uri"); envDBURI != "" {
		config.DBURI = envDBURI
	}
	if envDBKEYSURI := viper.GetString("db_keys_uri"); envDBKEYSURI != "" {
		config.DBKeysURI = envDBKEYSURI
	}
	if viper.IsSet("chat_storage_max_open_conns") {
		if n := viper.GetInt("chat_storage_max_open_conns"); n > 0 {
			config.ChatStorageMaxOpenConns = n
		}
	}

	// WhatsApp settings
	if envAutoReply := viper.GetString("whatsapp_auto_reply"); envAutoReply != "" {
		config.WhatsappAutoReplyMessage = envAutoReply
	}
	if viper.IsSet("whatsapp_auto_mark_read") {
		config.WhatsappAutoMarkRead = viper.GetBool("whatsapp_auto_mark_read")
	}
	if viper.IsSet("whatsapp_auto_download_media") {
		config.WhatsappAutoDownloadMedia = viper.GetBool("whatsapp_auto_download_media")
	}
	if envWebhook := viper.GetString("whatsapp_webhook"); envWebhook != "" {
		webhook := strings.Split(envWebhook, ",")
		config.WhatsappWebhook = webhook
	}
	if envWebhookSecret := viper.GetString("whatsapp_webhook_secret"); envWebhookSecret != "" {
		config.WhatsappWebhookSecret = envWebhookSecret
	}
	if viper.IsSet("whatsapp_webhook_insecure_skip_verify") {
		config.WhatsappWebhookInsecureSkipVerify = viper.GetBool("whatsapp_webhook_insecure_skip_verify")
	}
	if envWebhookEvents := viper.GetString("whatsapp_webhook_events"); envWebhookEvents != "" {
		events := strings.Split(envWebhookEvents, ",")
		config.WhatsappWebhookEvents = events
	}
	if envWebhookIgnoreJids := viper.GetString("whatsapp_webhook_ignore_jids"); envWebhookIgnoreJids != "" {
		parts := strings.Split(envWebhookIgnoreJids, ",")
		jids := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				jids = append(jids, trimmed)
			}
		}
		config.WhatsappWebhookIgnoreJids = jids
	}
	if viper.IsSet("whatsapp_account_validation") {
		config.WhatsappAccountValidation = viper.GetBool("whatsapp_account_validation")
	}
	if viper.IsSet("whatsapp_auto_reject_call") {
		config.WhatsappAutoRejectCall = viper.GetBool("whatsapp_auto_reject_call")
	}
	if envPresenceOnConnect := viper.GetString("whatsapp_presence_on_connect"); envPresenceOnConnect != "" {
		config.WhatsappPresenceOnConnect = envPresenceOnConnect
	}
	// Outbound proxy for whatsmeow WebSocket. Standard HTTP_PROXY env does not
	// apply to the underlying ws dialer; this binding plumbs the address into
	// (*whatsmeow.Client).SetProxyAddress before Connect. See issue #581.
	if envProxy := viper.GetString("whatsapp_proxy"); envProxy != "" {
		config.WhatsappProxy = envProxy
	}
	if viper.IsSet("whatsapp_presence_pulse_enabled") {
		config.WhatsappPresencePulseEnabled = viper.GetBool("whatsapp_presence_pulse_enabled")
	}
	if viper.IsSet("whatsapp_presence_pulse_interval") {
		if interval := viper.GetDuration("whatsapp_presence_pulse_interval"); interval > 0 {
			config.WhatsappPresencePulseInterval = interval
		}
	}
	if viper.IsSet("whatsapp_presence_pulse_duration") {
		if duration := viper.GetDuration("whatsapp_presence_pulse_duration"); duration > 0 {
			config.WhatsappPresencePulseDuration = duration
		}
	}

	// Chatwoot settings
	if viper.IsSet("chatwoot_enabled") {
		config.ChatwootEnabled = viper.GetBool("chatwoot_enabled")
	}
	if envChatwootURL := viper.GetString("chatwoot_url"); envChatwootURL != "" {
		config.ChatwootURL = envChatwootURL
	}
	if envChatwootAPIToken := viper.GetString("chatwoot_api_token"); envChatwootAPIToken != "" {
		config.ChatwootAPIToken = envChatwootAPIToken
	}
	if viper.IsSet("chatwoot_account_id") {
		config.ChatwootAccountID = viper.GetInt("chatwoot_account_id")
	}
	if viper.IsSet("chatwoot_inbox_id") {
		config.ChatwootInboxID = viper.GetInt("chatwoot_inbox_id")
	}
	if envChatwootDeviceID := viper.GetString("chatwoot_device_id"); envChatwootDeviceID != "" {
		config.ChatwootDeviceID = envChatwootDeviceID
	}
	// Chatwoot History Sync settings
	if viper.IsSet("chatwoot_import_messages") {
		config.ChatwootImportMessages = viper.GetBool("chatwoot_import_messages")
	}
	if viper.IsSet("chatwoot_days_limit_import_messages") {
		config.ChatwootDaysLimitImportMessages = viper.GetInt("chatwoot_days_limit_import_messages")
	}
	if envChatwootImportDBURI := viper.GetString("chatwoot_import_db_uri"); envChatwootImportDBURI != "" {
		config.ChatwootImportDBURI = envChatwootImportDBURI
	}
	if viper.IsSet("chatwoot_import_placeholder_media_message") {
		config.ChatwootImportPlaceholderMediaMessage = viper.GetBool("chatwoot_import_placeholder_media_message")
	}
	if viper.IsSet("chatwoot_import_media_with_rest") {
		config.ChatwootImportMediaWithREST = viper.GetBool("chatwoot_import_media_with_rest")
	}
	// Chatwoot auto-provisioning settings
	if viper.IsSet("chatwoot_auto_create") {
		config.ChatwootAutoCreate = viper.GetBool("chatwoot_auto_create")
	}
	if envChatwootInboxName := viper.GetString("chatwoot_inbox_name"); envChatwootInboxName != "" {
		config.ChatwootInboxName = envChatwootInboxName
	}
	if envChatwootWebhookURL := viper.GetString("chatwoot_webhook_url"); envChatwootWebhookURL != "" {
		config.ChatwootWebhookURL = envChatwootWebhookURL
	}
	if envChatwootWebhookSecret := viper.GetString("chatwoot_webhook_secret"); envChatwootWebhookSecret != "" {
		config.ChatwootWebhookSecret = envChatwootWebhookSecret
	}
	if envChatwootAllowedHosts := viper.GetString("chatwoot_allowed_hosts"); envChatwootAllowedHosts != "" {
		config.ChatwootAllowedHosts = splitCommaTrimmed(envChatwootAllowedHosts)
	}
	// Chatwoot conversation handling settings
	if viper.IsSet("chatwoot_reopen_conversation") {
		config.ChatwootReopenConversation = viper.GetBool("chatwoot_reopen_conversation")
	}
	if viper.IsSet("chatwoot_conversation_pending") {
		config.ChatwootConversationPending = viper.GetBool("chatwoot_conversation_pending")
	}
	if envChatwootIgnoreJids := viper.GetString("chatwoot_ignore_jids"); envChatwootIgnoreJids != "" {
		config.ChatwootIgnoreJids = splitCommaTrimmed(envChatwootIgnoreJids)
	}
	// Chatwoot outbound signature settings
	if viper.IsSet("chatwoot_sign_msg") {
		config.ChatwootSignMsg = viper.GetBool("chatwoot_sign_msg")
	}
	if envChatwootSignDelimiter := viper.GetString("chatwoot_sign_delimiter"); envChatwootSignDelimiter != "" {
		config.ChatwootSignDelimiter = envChatwootSignDelimiter
	}
	// Chatwoot edit/delete propagation settings
	if viper.IsSet("chatwoot_forward_edits") {
		config.ChatwootForwardEdits = viper.GetBool("chatwoot_forward_edits")
	}
	if viper.IsSet("chatwoot_forward_deletes") {
		config.ChatwootForwardDeletes = viper.GetBool("chatwoot_forward_deletes")
	}
	if viper.IsSet("chatwoot_message_read") {
		config.ChatwootMessageRead = viper.GetBool("chatwoot_message_read")
	}
	if viper.IsSet("chatwoot_message_delete") {
		config.ChatwootMessageDelete = viper.GetBool("chatwoot_message_delete")
	}
}

func initFlags() {
	// Application flags
	rootCmd.PersistentFlags().StringVarP(
		&config.AppPort,
		"port", "p",
		config.AppPort,
		"change port number with --port <number> | example: --port=8080",
	)

	rootCmd.PersistentFlags().StringVarP(
		&config.AppHost,
		"host", "H",
		config.AppHost,
		`host to bind the server --host <string> | example: --host="127.0.0.1"`,
	)

	rootCmd.PersistentFlags().BoolVarP(
		&config.AppDebug,
		"debug", "d",
		config.AppDebug,
		"hide or displaying log with --debug <true/false> | example: --debug=true",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AppOs,
		"os", "",
		config.AppOs,
		`os name --os <string> | example: --os="Chrome"`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.AppBasicAuthCredential,
		"basic-auth", "b",
		config.AppBasicAuthCredential,
		"basic auth credential | -b=yourUsername:yourPassword",
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AppBasePath,
		"base-path", "",
		config.AppBasePath,
		`base path for subpath deployment --base-path <string> | example: --base-path="/gowa"`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.AppTrustedProxies,
		"trusted-proxies", "",
		config.AppTrustedProxies,
		`trusted proxy IP ranges for reverse proxy deployments --trusted-proxies <string> | example: --trusted-proxies="0.0.0.0/0" or --trusted-proxies="10.0.0.0/8,172.16.0.0/12"`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.AppCORSAllowedOrigins,
		"cors-allowed-origins", "",
		config.AppCORSAllowedOrigins,
		`allowed CORS origins, any origin when empty --cors-allowed-origins <string> | example: --cors-allowed-origins="https://ui.example.com,https://ops.example.com"`,
	)

	// Web UI flags
	rootCmd.PersistentFlags().BoolVarP(
		&config.AppUIEnabled,
		"ui-enabled", "",
		config.AppUIEnabled,
		`serve the gowa-ui dashboard at "/" --ui-enabled <bool>`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.AppUIAutoUpdate,
		"ui-auto-update", "",
		config.AppUIAutoUpdate,
		`download the latest gowa-ui release at startup and periodically --ui-auto-update <bool> | disable for air-gapped deployments with a pre-seeded storages/ui cache`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AppUIRepo,
		"ui-repo", "",
		config.AppUIRepo,
		`GitHub repository the dashboard is downloaded from --ui-repo <string> | example: --ui-repo="aldinokemal/gowa-ui"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AppUIAssetName,
		"ui-asset-name", "",
		config.AppUIAssetName,
		`release asset name to download --ui-asset-name <string>`,
	)
	rootCmd.PersistentFlags().DurationVarP(
		&config.AppUIUpdateInterval,
		"ui-update-interval", "",
		config.AppUIUpdateInterval,
		`how often to check for a newer dashboard release --ui-update-interval <duration> | example: --ui-update-interval=3h`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.AppUIAssetSHA256,
		"ui-asset-sha256", "",
		config.AppUIAssetSHA256,
		`supply-chain pin: only serve the dashboard whose sha256 matches --ui-asset-sha256 <hex> (see the release's .sha256 asset)`,
	)

	// Database flags
	rootCmd.PersistentFlags().StringVarP(
		&config.DBURI,
		"db-uri", "",
		config.DBURI,
		`the database uri to store the connection data database uri (by default, we'll use sqlite3 under storages/whatsapp.db). database uri --db-uri <string> | example: --db-uri="file:storages/whatsapp.db?_foreign_keys=on or postgres://user:password@localhost:5432/whatsapp"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.DBKeysURI,
		"db-keys-uri", "",
		config.DBKeysURI,
		`the database uri to store the optional keys cache (by default, we'll use the same database uri). avoid in-memory storage in production. database uri --db-keys-uri <string> | example: --db-keys-uri="file:storages/whatsapp-keys.db?_foreign_keys=on"`,
	)

	// WhatsApp flags
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappAutoReplyMessage,
		"autoreply", "",
		config.WhatsappAutoReplyMessage,
		`auto reply when received message --autoreply <string> | example: --autoreply="Don't reply this message"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAutoMarkRead,
		"auto-mark-read", "",
		config.WhatsappAutoMarkRead,
		`auto mark incoming messages as read --auto-mark-read <true/false> | example: --auto-mark-read=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAutoDownloadMedia,
		"auto-download-media", "",
		config.WhatsappAutoDownloadMedia,
		`auto download media from incoming messages --auto-download-media <true/false> | example: --auto-download-media=false`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.WhatsappWebhook,
		"webhook", "w",
		config.WhatsappWebhook,
		`forward event to webhook --webhook <string> | example: --webhook="https://yourcallback.com/callback"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappWebhookSecret,
		"webhook-secret", "",
		config.WhatsappWebhookSecret,
		`secure webhook request --webhook-secret <string> | example: --webhook-secret="super-secret-key"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappWebhookInsecureSkipVerify,
		"webhook-insecure-skip-verify", "",
		config.WhatsappWebhookInsecureSkipVerify,
		`skip TLS certificate verification for webhooks (INSECURE - use only for development/self-signed certs) --webhook-insecure-skip-verify <true/false> | example: --webhook-insecure-skip-verify=true`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.WhatsappWebhookEvents,
		"webhook-events", "",
		config.WhatsappWebhookEvents,
		`whitelist of events to forward to webhook (empty = all events) --webhook-events <string> | example: --webhook-events="message,message.ack,group.participants"`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.WhatsappWebhookIgnoreJids,
		"webhook-ignore-jids", "",
		config.WhatsappWebhookIgnoreJids,
		`comma-separated WhatsApp JIDs (or "@g.us"/"@s.whatsapp.net"/"@lid" wildcards) to skip when forwarding to webhooks --webhook-ignore-jids <list> | example: --webhook-ignore-jids="@g.us,628123456789@s.whatsapp.net"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAccountValidation,
		"account-validation", "",
		config.WhatsappAccountValidation,
		`enable or disable account validation --account-validation <true/false> | example: --account-validation=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappAutoRejectCall,
		"auto-reject-call", "",
		config.WhatsappAutoRejectCall,
		`auto reject incoming calls --auto-reject-call <true/false> | example: --auto-reject-call=true`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappPresenceOnConnect,
		"presence-on-connect", "",
		config.WhatsappPresenceOnConnect,
		`presence to send on connect: "available", "unavailable", or "none" --presence-on-connect <string> | example: --presence-on-connect="unavailable"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappProxy,
		"whatsapp-proxy", "",
		config.WhatsappProxy,
		`outbound proxy for the WhatsApp WebSocket dialer --whatsapp-proxy <string> | example: --whatsapp-proxy="socks5://user:pass@host:1080"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappPresencePulseEnabled,
		"presence-pulse-enabled", "",
		config.WhatsappPresencePulseEnabled,
		`enable daily presence pulse --presence-pulse-enabled <true/false> | example: --presence-pulse-enabled=true`,
	)
	rootCmd.PersistentFlags().DurationVarP(
		&config.WhatsappPresencePulseInterval,
		"presence-pulse-interval", "",
		config.WhatsappPresencePulseInterval,
		`presence pulse interval --presence-pulse-interval <duration> | example: --presence-pulse-interval=24h`,
	)
	rootCmd.PersistentFlags().DurationVarP(
		&config.WhatsappPresencePulseDuration,
		"presence-pulse-duration", "",
		config.WhatsappPresencePulseDuration,
		`duration to stay available during a presence pulse --presence-pulse-duration <duration> | example: --presence-pulse-duration=5m`,
	)

	// Chatwoot flags
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootEnabled,
		"chatwoot-enabled", "",
		config.ChatwootEnabled,
		`enable Chatwoot integration --chatwoot-enabled <true/false> | example: --chatwoot-enabled=true`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.ChatwootDeviceID,
		"chatwoot-device-id", "",
		config.ChatwootDeviceID,
		`device ID for Chatwoot outbound messages --chatwoot-device-id <string> | example: --chatwoot-device-id="my-device"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootImportMessages,
		"chatwoot-import-messages", "",
		config.ChatwootImportMessages,
		`enable message history import to Chatwoot --chatwoot-import-messages <true/false> | example: --chatwoot-import-messages=true`,
	)
	rootCmd.PersistentFlags().IntVarP(
		&config.ChatwootDaysLimitImportMessages,
		"chatwoot-days-limit-import-messages", "",
		config.ChatwootDaysLimitImportMessages,
		`days of message history to import to Chatwoot --chatwoot-days-limit-import-messages <int> | example: --chatwoot-days-limit-import-messages=7`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.ChatwootImportDBURI,
		"chatwoot-import-db-uri", "",
		config.ChatwootImportDBURI,
		`Postgres URI for direct Chatwoot history import. When set, historical sync bypasses the REST API and INSERTs directly into Chatwoot's database, preserving original WhatsApp timestamps and metadata. Live messages still use REST. Example: --chatwoot-import-db-uri="postgresql://postgres:pass@localhost:5432/chatwoot_production?sslmode=disable"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootImportPlaceholderMediaMessage,
		"chatwoot-import-placeholder-media-message", "",
		config.ChatwootImportPlaceholderMediaMessage,
		`insert a placeholder body for media messages when media download fails during direct-DB import --chatwoot-import-placeholder-media-message <true/false> | example: --chatwoot-import-placeholder-media-message=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootImportMediaWithREST,
		"chatwoot-import-media-with-rest", "",
		config.ChatwootImportMediaWithREST,
		`upload media history rows through Chatwoot REST while direct-DB import handles non-media rows --chatwoot-import-media-with-rest <true/false> | example: --chatwoot-import-media-with-rest=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootAutoCreate,
		"chatwoot-auto-create", "",
		config.ChatwootAutoCreate,
		`auto-create (or reuse) the Chatwoot API inbox on startup and resolve the inbox id automatically --chatwoot-auto-create <true/false> | example: --chatwoot-auto-create=true`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.ChatwootInboxName,
		"chatwoot-inbox-name", "",
		config.ChatwootInboxName,
		`name of the Chatwoot inbox to create/reuse when auto-create is enabled --chatwoot-inbox-name <string> | example: --chatwoot-inbox-name="WhatsApp"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.ChatwootWebhookURL,
		"chatwoot-webhook-url", "",
		config.ChatwootWebhookURL,
		`public URL of this app's /chatwoot/webhook endpoint, registered on the auto-created inbox --chatwoot-webhook-url <string> | example: --chatwoot-webhook-url="https://my-api.com/chatwoot/webhook"`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.ChatwootWebhookSecret,
		"chatwoot-webhook-secret", "",
		config.ChatwootWebhookSecret,
		`shared secret required for incoming Chatwoot webhooks --chatwoot-webhook-secret <string> | example: --chatwoot-webhook-secret="super-secret-key"`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.ChatwootAllowedHosts,
		"chatwoot-allowed-hosts", "",
		config.ChatwootAllowedHosts,
		`comma-separated allowlist of Chatwoot hosts a per-device config may target (hardens SSRF surface; empty = allow any public host) --chatwoot-allowed-hosts <list> | example: --chatwoot-allowed-hosts="app.chatwoot.com,chat.example.com"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootReopenConversation,
		"chatwoot-reopen-conversation", "",
		config.ChatwootReopenConversation,
		`reuse and reopen a resolved Chatwoot conversation for a returning contact instead of opening a new one --chatwoot-reopen-conversation <true/false> | example: --chatwoot-reopen-conversation=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootConversationPending,
		"chatwoot-conversation-pending", "",
		config.ChatwootConversationPending,
		`open newly-created Chatwoot conversations in "pending" instead of "open" status --chatwoot-conversation-pending <true/false> | example: --chatwoot-conversation-pending=true`,
	)
	rootCmd.PersistentFlags().StringSliceVarP(
		&config.ChatwootIgnoreJids,
		"chatwoot-ignore-jids", "",
		config.ChatwootIgnoreJids,
		`comma-separated WhatsApp JIDs (or "@g.us"/"@s.whatsapp.net" wildcards) to never mirror to Chatwoot --chatwoot-ignore-jids <list> | example: --chatwoot-ignore-jids="@g.us,123@s.whatsapp.net"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootSignMsg,
		"chatwoot-sign-msg", "",
		config.ChatwootSignMsg,
		`prefix Chatwoot agent replies with the agent's name before delivery to WhatsApp --chatwoot-sign-msg <true/false> | example: --chatwoot-sign-msg=true`,
	)
	rootCmd.PersistentFlags().StringVarP(
		&config.ChatwootSignDelimiter,
		"chatwoot-sign-delimiter", "",
		config.ChatwootSignDelimiter,
		`delimiter inserted between the agent signature and the message body --chatwoot-sign-delimiter <string> | example: --chatwoot-sign-delimiter="\n\n"`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootForwardEdits,
		"chatwoot-forward-edits", "",
		config.ChatwootForwardEdits,
		`mirror WhatsApp message edits into the Chatwoot conversation as threaded notes --chatwoot-forward-edits <true/false> | example: --chatwoot-forward-edits=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootForwardDeletes,
		"chatwoot-forward-deletes", "",
		config.ChatwootForwardDeletes,
		`mirror WhatsApp delete-for-everyone events into the Chatwoot conversation as threaded notes --chatwoot-forward-deletes <true/false> | example: --chatwoot-forward-deletes=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootMessageRead,
		"chatwoot-message-read", "",
		config.ChatwootMessageRead,
		`sync read state between WhatsApp and Chatwoot for linked messages --chatwoot-message-read <true/false> | example: --chatwoot-message-read=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.ChatwootMessageDelete,
		"chatwoot-message-delete", "",
		config.ChatwootMessageDelete,
		`delete linked Chatwoot/WhatsApp messages when deletion is reported by the opposite side --chatwoot-message-delete <true/false> | example: --chatwoot-message-delete=true`,
	)
}

func initChatStorage() (*sql.DB, error) {
	connStr := sqlite.FormatChatStorageURI(config.ChatStorageURI, config.ChatStorageEnableWAL, config.ChatStorageEnableForeignKeys)

	db, err := sql.Open(sqlite.DriverName, connStr)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	maxConns := config.ChatStorageMaxOpenConns
	if maxConns < 1 {
		maxConns = 1
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func initApp() {
	if config.AppDebug {
		config.WhatsappLogLevel = "DEBUG"
		logrus.SetLevel(logrus.DebugLevel)
	}

	//preparing folder if not exist
	err := utils.CreateFolder(config.PathQrCode, config.PathSendItems, config.PathStorages, config.PathMedia, config.PathUICache)
	if err != nil {
		logrus.Errorln(err)
	}

	ctx := context.Background()

	chatStorageDB, err = initChatStorage()
	if err != nil {
		// Terminate the application if chat storage fails to initialize to avoid nil pointer panics later.
		logrus.Fatalf("failed to initialize chat storage: %v", err)
	}

	chatStorageRepo = chatstorage.NewStorageRepository(chatStorageDB)
	chatStorageRepo.InitializeSchema()

	whatsappDB := whatsapp.InitWaDB(ctx, config.DBURI)
	var keysDB *sqlstore.Container
	if config.DBKeysURI != "" {
		keysDB = whatsapp.InitWaDB(ctx, config.DBKeysURI)
	}

	whatsappCli = whatsapp.InitWaCLI(ctx, whatsappDB, keysDB, chatStorageRepo)

	// Initialize device manager and usecase for multi-device support
	dm := whatsapp.GetDeviceManager()
	if dm != nil {
		_ = dm.LoadExistingDevices(ctx)
	}

	// Usecase
	appUsecase = usecase.NewAppService(chatStorageRepo, dm)
	callUsecase = usecase.NewCallService()
	chatUsecase = usecase.NewChatService(chatStorageRepo)
	sendUsecase = usecase.NewSendService(appUsecase, chatStorageRepo)
	userUsecase = usecase.NewUserService(chatStorageRepo)
	messageUsecase = usecase.NewMessageService(chatStorageRepo)
	groupUsecase = usecase.NewGroupService()
	newsletterUsecase = usecase.NewNewsletterService()
	deviceUsecase = usecase.NewDeviceService(dm, appUsecase)
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// splitCommaTrimmed splits a comma-separated env value into trimmed, non-empty
// entries. Shared by the Chatwoot ignore-jids and allowed-hosts settings.
func splitCommaTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
