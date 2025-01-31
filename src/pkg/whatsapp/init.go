package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/internal/websocket"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var (
	cli           *whatsmeow.Client
	log           waLog.Logger
	historySyncID int32
	startupTime   = time.Now().Unix()
)

type ExtractedMedia struct {
	MediaPath string `json:"media_path"`
	MimeType  string `json:"mime_type"`
	Caption   string `json:"caption"`
}

type evtReaction struct {
	ID      string `json:"id,omitempty"`
	Message string `json:"message,omitempty"`
}

type evtMessage struct {
	ID            string `json:"id,omitempty"`
	Text          string `json:"text,omitempty"`
	RepliedId     string `json:"replied_id,omitempty"`
	QuotedMessage string `json:"quoted_message,omitempty"`
}

func InitWaDB() *sqlstore.Container {
	// Running Whatsapp
	log = waLog.Stdout("Main", config.WhatsappLogLevel, true)
	dbLog := waLog.Stdout("Database", config.WhatsappLogLevel, true)

	var storeContainer *sqlstore.Container
	var err error
	if strings.HasPrefix(config.DBURI, "file:") {
		storeContainer, err = sqlstore.New("sqlite3", config.DBURI, dbLog)
	} else if strings.HasPrefix(config.DBURI, "postgres:") {
		storeContainer, err = sqlstore.New("postgres", config.DBURI, dbLog)
	} else {
		log.Errorf("Unknown database type: %s", config.DBURI)
		panic(pkgError.InternalServerError(fmt.Sprintf("Unknown database type: %s. Currently only sqlite3(file:) and postgres are supported", config.DBURI)))
	}
	if err != nil {
		log.Errorf("Failed to connect to database: %v", err)
		panic(pkgError.InternalServerError(fmt.Sprintf("Failed to connect to database: %v", err)))

	}
	return storeContainer
}

func InitWaCLI(storeContainer *sqlstore.Container) *whatsmeow.Client {
	device, err := storeContainer.GetFirstDevice()
	if err != nil {
		log.Errorf("Failed to get device: %v", err)
		panic(err)
	}

	if device == nil {
		log.Errorf("No device found")
		panic("No device found")
	}

	osName := fmt.Sprintf("%s %s", config.AppOs, config.AppVersion)
	store.DeviceProps.PlatformType = &config.AppPlatform
	store.DeviceProps.Os = &osName
	cli = whatsmeow.NewClient(device, waLog.Stdout("Client", config.WhatsappLogLevel, true))
	cli.EnableAutoReconnect = true
	cli.AutoTrustIdentity = true
	cli.AddEventHandler(handler)

	return cli
}

func handler(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
	case *events.DeleteForMe:
		log.Infof("Deleted message %s for %s", evt.MessageID, evt.SenderJID.String())
	case *events.AppStateSyncComplete:
		if len(cli.Store.PushName) > 0 && evt.Name == appstate.WAPatchCriticalBlock {
			err := cli.SendPresence(types.PresenceAvailable)
			if err != nil {
				log.Warnf("Failed to send available presence: %v", err)
			} else {
				log.Infof("Marked self as available")
			}
		}
	case *events.PairSuccess:
		websocket.Broadcast <- websocket.BroadcastMessage{
			Code:    "LOGIN_SUCCESS",
			Message: fmt.Sprintf("Successfully pair with %s", evt.ID.String()),
		}
	case *events.LoggedOut:
		websocket.Broadcast <- websocket.BroadcastMessage{
			Code:   "LIST_DEVICES",
			Result: nil,
		}
	case *events.Connected, *events.PushNameSetting:
		if len(cli.Store.PushName) == 0 {
			return
		}

		// Send presence available when connecting and when the pushname is changed.
		// This makes sure that outgoing messages always have the right pushname.
		err := cli.SendPresence(types.PresenceAvailable)
		if err != nil {
			log.Warnf("Failed to send available presence: %v", err)
		} else {
			log.Infof("Marked self as available")
		}
	case *events.StreamReplaced:
		os.Exit(0)
	case *events.Message:
		metaParts := []string{
			fmt.Sprintf("pushname: %s", evt.Info.PushName),
			fmt.Sprintf("timestamp: %s", evt.Info.Timestamp),
		}
		if evt.Info.Type != "" {
			metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
		}
		if evt.Info.Category != "" {
			metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "view once")
		}

		log.Infof("Received message %s from %s (%s): %+v", evt.Info.ID, evt.Info.SourceString(), strings.Join(metaParts, ", "), evt.Message)
		message := ExtractMessageText(evt)
		utils.RecordMessage(evt.Info.ID, evt.Info.Sender.String(), message)

		if img := evt.Message.GetImageMessage(); img != nil {
			if path, err := ExtractMedia(config.PathStorages, img); err != nil {
				log.Errorf("Failed to download image: %v", err)
			} else {
				log.Infof("Image downloaded to %s", path)
			}
		}

		if config.WhatsappAutoReplyMessage != "" &&
			!isGroupJid(evt.Info.Chat.String()) &&
			!strings.Contains(evt.Info.SourceString(), "broadcast") {
			_, _ = cli.SendMessage(context.Background(), evt.Info.Sender, &waE2E.Message{Conversation: proto.String(config.WhatsappAutoReplyMessage)})
		}

		if len(config.WhatsappWebhook) > 0 &&
			!strings.Contains(evt.Info.SourceString(), "broadcast") &&
			!isFromMySelf(evt.Info.SourceString()) {
			go func(evt *events.Message) {
				if err := forwardToWebhook(evt); err != nil {
					logrus.Error("Failed forward to webhook: ", err)
				}
			}(evt)
		}
	case *events.Receipt:
		if evt.Type == types.ReceiptTypeRead || evt.Type == types.ReceiptTypeReadSelf {
			log.Infof("%v was read by %s at %s", evt.MessageIDs, evt.SourceString(), evt.Timestamp)
		} else if evt.Type == types.ReceiptTypeDelivered {
			log.Infof("%s was delivered to %s at %s", evt.MessageIDs[0], evt.SourceString(), evt.Timestamp)
		}
	case *events.Presence:
		if evt.Unavailable {
			if evt.LastSeen.IsZero() {
				log.Infof("%s is now offline", evt.From)
			} else {
				log.Infof("%s is now offline (last seen: %s)", evt.From, evt.LastSeen)
			}
		} else {
			log.Infof("%s is now online", evt.From)
		}
	case *events.HistorySync:
		id := atomic.AddInt32(&historySyncID, 1)
		fileName := fmt.Sprintf("%s/history-%d-%s-%d-%s.json", config.PathStorages, startupTime, cli.Store.ID.String(), id, evt.Data.SyncType.String())
		file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			log.Errorf("Failed to open file to write history sync: %v", err)
			return
		}
		enc := json.NewEncoder(file)
		enc.SetIndent("", "  ")
		err = enc.Encode(evt.Data)
		if err != nil {
			log.Errorf("Failed to write history sync: %v", err)
			return
		}
		log.Infof("Wrote history sync to %s", fileName)
		_ = file.Close()
	case *events.AppState:
		log.Debugf("App state event: %+v / %+v", evt.Index, evt.SyncActionValue)
	}
}
