package cmd

import (
	"context"
	"embed"
	"os"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

var (
	EmbedIndex embed.FS
	EmbedViews embed.FS

	// Whatsapp
	whatsappCli *whatsmeow.Client
	whatsappDB  *sqlstore.Container

	// Usecase
	appUsecase        domainApp.IAppUsecase
	sendUsecase       domainSend.ISendUsecase
	userUsecase       domainUser.IUserUsecase
	messageUsecase    domainMessage.IMessageUsecase
	groupUsecase      domainGroup.IGroupUsecase
	newsletterUsecase domainNewsletter.INewsletterUsecase
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
	// Application settings
	if envPort := viper.GetString("APP_PORT"); envPort != "" {
		config.AppPort = envPort
	}
	if envDebug := viper.GetBool("APP_DEBUG"); envDebug {
		config.AppDebug = envDebug
	}
	if envOs := viper.GetString("APP_OS"); envOs != "" {
		config.AppOs = envOs
	}
	if envBasicAuth := viper.GetString("APP_BASIC_AUTH"); envBasicAuth != "" {
		credential := strings.Split(envBasicAuth, ",")
		config.AppBasicAuthCredential = credential
	}
	if envChatFlushInterval := viper.GetInt("APP_CHAT_FLUSH_INTERVAL"); envChatFlushInterval > 0 {
		config.AppChatFlushIntervalDays = envChatFlushInterval
	}

	// Database settings
	if envDBURI := viper.GetString("DB_URI"); envDBURI != "" {
		config.DBURI = envDBURI
	}

	// WhatsApp settings
	if envAutoReply := viper.GetString("WHATSAPP_AUTO_REPLY"); envAutoReply != "" {
		config.WhatsappAutoReplyMessage = envAutoReply
	}
	if envWebhook := viper.GetString("WHATSAPP_WEBHOOK"); envWebhook != "" {
		webhook := strings.Split(envWebhook, ",")
		config.WhatsappWebhook = webhook
	}
	if envWebhookSecret := viper.GetString("WHATSAPP_WEBHOOK_SECRET"); envWebhookSecret != "" {
		config.WhatsappWebhookSecret = envWebhookSecret
	}
	if envAccountValidation := viper.GetBool("WHATSAPP_ACCOUNT_VALIDATION"); envAccountValidation {
		config.WhatsappAccountValidation = envAccountValidation
	}
	if envChatStorage := viper.GetBool("WHATSAPP_CHAT_STORAGE"); !envChatStorage {
		config.WhatsappChatStorage = envChatStorage
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
	rootCmd.PersistentFlags().IntVarP(
		&config.AppChatFlushIntervalDays,
		"chat-flush-interval", "",
		config.AppChatFlushIntervalDays,
		`the interval to flush the chat storage --chat-flush-interval <number> | example: --chat-flush-interval=7`,
	)

	// Database flags
	rootCmd.PersistentFlags().StringVarP(
		&config.DBURI,
		"db-uri", "",
		config.DBURI,
		`the database uri to store the connection data database uri (by default, we'll use sqlite3 under storages/whatsapp.db). database uri --db-uri <string> | example: --db-uri="file:storages/whatsapp.db?_foreign_keys=on or postgres://user:password@localhost:5432/whatsapp"`,
	)

	// WhatsApp flags
	rootCmd.PersistentFlags().StringVarP(
		&config.WhatsappAutoReplyMessage,
		"autoreply", "",
		config.WhatsappAutoReplyMessage,
		`auto reply when received message --autoreply <string> | example: --autoreply="Don't reply this message"`,
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
		&config.WhatsappAccountValidation,
		"account-validation", "",
		config.WhatsappAccountValidation,
		`enable or disable account validation --account-validation <true/false> | example: --account-validation=true`,
	)
	rootCmd.PersistentFlags().BoolVarP(
		&config.WhatsappChatStorage,
		"chat-storage", "",
		config.WhatsappChatStorage,
		`enable or disable chat storage --chat-storage <true/false>. If you disable this, reply feature maybe not working properly | example: --chat-storage=true`,
	)
}

func initApp() {
	if config.AppDebug {
		config.WhatsappLogLevel = "DEBUG"
	}
	//preparing folder if not exist
	err := utils.CreateFolder(config.PathQrCode, config.PathSendItems, config.PathStorages, config.PathMedia)
	if err != nil {
		logrus.Errorln(err)
	}

	ctx := context.Background()
	whatsappDB = whatsapp.InitWaDB(ctx)
	whatsappCli = whatsapp.InitWaCLI(ctx, whatsappDB)

	// Usecase
	appUsecase = usecase.NewAppService(whatsappCli, whatsappDB)
	sendUsecase = usecase.NewSendService(whatsappCli, appUsecase)
	userUsecase = usecase.NewUserService(whatsappCli)
	messageUsecase = usecase.NewMessageService(whatsappCli)
	groupUsecase = usecase.NewGroupService(whatsappCli)
	newsletterUsecase = usecase.NewNewsletterService(whatsappCli)
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute(embedIndex embed.FS, embedViews embed.FS) {
	EmbedIndex = embedIndex
	EmbedViews = embedViews
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
