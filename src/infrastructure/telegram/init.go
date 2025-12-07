package telegram

import (
	"fmt"
	"net/http"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/sirupsen/logrus"
)

var (
	Bot       *gotgbot.Bot
	Updater   *ext.Updater
	repo      domainChatStorage.IChatStorageRepository
)

// InitTelegram initializes the Telegram bot
func InitTelegram(chatStorageRepo domainChatStorage.IChatStorageRepository) error {
	if config.TelegramBotToken == "" {
		logrus.Info("Telegram Bot Token not set, skipping Telegram integration")
		return nil
	}

	repo = chatStorageRepo

	var err error
	Bot, err = gotgbot.NewBot(config.TelegramBotToken, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{
				Timeout: time.Second * 30,
			},
			DefaultRequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 30,
				APIURL:  gotgbot.DefaultAPIURL,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create telegram bot: %w", err)
	}

	logrus.Infof("Telegram Bot initialized: %s", Bot.User.Username)

	// Register Bridge function
	whatsapp.TelegramBridgeFunc = BridgeMessageToTelegram

	// Create dispatcher
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		// If an error occurs, print it to the console
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			logrus.Errorf("failed to handle update: %v", err)
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	// Create updater
	Updater = ext.NewUpdater(dispatcher, nil)

	// Handle messages from supergroup topics
	dispatcher.AddHandler(handlers.NewMessage(
		func(msg *gotgbot.Message) bool {
			// Check if message is from target group
			if msg.Chat.Id != config.TelegramTargetGroupID {
				return false
			}
			// Check if it's a topic message
			if msg.MessageThreadId == 0 {
				return false
			}
			// Check allowed users
			if len(config.TelegramAllowedUsers) > 0 {
				allowed := false
				for _, uid := range config.TelegramAllowedUsers {
					if msg.From.Id == uid {
						allowed = true
						break
					}
				}
				if !allowed {
					return false
				}
			}
			return true
		},
		HandleTopicMessage,
	))

	// Start polling
	err = Updater.StartPolling(Bot, &ext.PollingOpts{
		DropPendingUpdates: true,
		GetUpdatesOpts: &gotgbot.GetUpdatesOpts{
			Timeout: 9,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: time.Second * 10,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to start polling: %w", err)
	}

	logrus.Info("Telegram Bot polling started")
	return nil
}
