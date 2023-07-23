package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/internal/websocket"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	"mime"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

var (
	cli           *whatsmeow.Client
	log           waLog.Logger
	historySyncID int32
	startupTime   = time.Now().Unix()
)

func SanitizePhone(phone *string) {
	if phone != nil && len(*phone) > 0 && !strings.Contains(*phone, "@") {
		if len(*phone) <= 15 {
			*phone = fmt.Sprintf("%s@s.whatsapp.net", *phone)
		} else {
			*phone = fmt.Sprintf("%s@g.us", *phone)
		}
	}
}

func GetPlatformName(deviceID int) string {
	switch deviceID {
	case 0:
		return "UNKNOWN"
	case 1:
		return "CHROME"
	case 2:
		return "FIREFOX"
	case 3:
		return "IE"
	case 4:
		return "OPERA"
	case 5:
		return "SAFARI"
	case 6:
		return "EDGE"
	case 7:
		return "DESKTOP"
	case 8:
		return "IPAD"
	case 9:
		return "ANDROID_TABLET"
	case 10:
		return "OHANA"
	case 11:
		return "ALOHA"
	case 12:
		return "CATALINA"
	case 13:
		return "TCL_TV"
	default:
		return "UNKNOWN"
	}
}

func ParseJID(arg string) (types.JID, error) {
	if arg[0] == '+' {
		arg = arg[1:]
	}
	if !strings.ContainsRune(arg, '@') {
		return types.NewJID(arg, types.DefaultUserServer), nil
	} else {
		recipient, err := types.ParseJID(arg)
		if err != nil {
			fmt.Printf("invalid JID %s: %v", arg, err)
			return recipient, pkgError.ErrInvalidJID
		} else if recipient.User == "" {
			fmt.Printf("invalid JID %v: no server specified", arg)
			return recipient, pkgError.ErrInvalidJID
		}
		return recipient, nil
	}
}

func ValidateJidWithLogin(waCli *whatsmeow.Client, jid string) (types.JID, error) {
	MustLogin(waCli)
	return ParseJID(jid)
}

func InitWaDB() *sqlstore.Container {
	// Running Whatsapp
	log = waLog.Stdout("Main", config.WhatsappLogLevel, true)
	dbLog := waLog.Stdout("Database", config.WhatsappLogLevel, true)
	storeContainer, err := sqlstore.New("sqlite3", fmt.Sprintf("file:%s/%s?_foreign_keys=off", config.PathStorages, config.DBName), dbLog)
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

	osName := fmt.Sprintf("%s %s", config.AppOs, config.AppVersion)
	store.DeviceProps.PlatformType = &config.AppPlatform
	store.DeviceProps.Os = &osName
	cli = whatsmeow.NewClient(device, waLog.Stdout("Client", config.WhatsappLogLevel, true))
	cli.EnableAutoReconnect = true
	cli.AutoTrustIdentity = true
	cli.AddEventHandler(handler)

	return cli
}

func MustLogin(waCli *whatsmeow.Client) {
	if waCli == nil {
		panic(pkgError.InternalServerError("Whatsapp client is not initialized"))
	}
	if !waCli.IsConnected() {
		panic(pkgError.AuthError("you are not connect to services server, please reconnect"))
	} else if !waCli.IsLoggedIn() {
		panic(pkgError.AuthError("you are not login to services server, please login"))
	}
}

func handler(rawEvt interface{}) {
	switch evt := rawEvt.(type) {
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
		metaParts := []string{fmt.Sprintf("pushname: %s", evt.Info.PushName), fmt.Sprintf("timestamp: %s", evt.Info.Timestamp)}
		if evt.Info.Type != "" {
			metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
		}
		if evt.Info.Category != "" {
			metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "view once")
		}
		if evt.IsViewOnce {
			metaParts = append(metaParts, "ephemeral")
		}

		log.Infof("Received message %s from %s (%s): %+v", evt.Info.ID, evt.Info.SourceString(), strings.Join(metaParts, ", "), evt.Message)

		img := evt.Message.GetImageMessage()
		if img != nil {
			path, err := DownloadMedia(config.PathStorages, img)
			if err != nil {
				log.Errorf("Failed to download image: %v", err)
			} else {
				log.Infof("Image downloaded to %s", path)
			}
		}

		if !isGroupJid(evt.Info.Chat.String()) && !strings.Contains(evt.Info.SourceString(), "broadcast") {
			if config.WhatsappAutoReplyMessage != "" {
				_, _ = cli.SendMessage(context.Background(), evt.Info.Sender, &waProto.Message{Conversation: proto.String(config.WhatsappAutoReplyMessage)})
			}

			if config.WhatsappAutoReplyWebhook != "" {
				if err := sendAutoReplyWebhook(evt); err != nil {
					logrus.Error("Failed to send webhoook", err)
				}
			}
		}
	case *events.Receipt:
		if evt.Type == events.ReceiptTypeRead || evt.Type == events.ReceiptTypeReadSelf {
			log.Infof("%v was read by %s at %s", evt.MessageIDs, evt.SourceString(), evt.Timestamp)
		} else if evt.Type == events.ReceiptTypeDelivered {
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

func sendAutoReplyWebhook(evt *events.Message) error {
	logrus.Info("Sending webhook to", config.WhatsappAutoReplyWebhook)
	client := &http.Client{Timeout: 10 * time.Second}
	imageMedia := evt.Message.GetImageMessage()
	stickerMedia := evt.Message.GetStickerMessage()
	videoMedia := evt.Message.GetVideoMessage()
	audioMedia := evt.Message.GetAudioMessage()
	documentMedia := evt.Message.GetDocumentMessage()

	message := evt.Message.GetConversation()
	if extendedMessage := evt.Message.ExtendedTextMessage.GetText(); extendedMessage != "" {
		message = extendedMessage
	}

	var quotedmessage any
	if evt.Message.ExtendedTextMessage != nil {
		if conversation := evt.Message.ExtendedTextMessage.ContextInfo.QuotedMessage.GetConversation(); conversation != "" {
			quotedmessage = conversation
		}
	}

	var forwarded any
	if evt.Message.ExtendedTextMessage != nil && evt.Message.ExtendedTextMessage.ContextInfo != nil {
		if isForwarded := evt.Message.ExtendedTextMessage.ContextInfo.GetIsForwarded(); !isForwarded {
			forwarded = nil
		}
	}

	var reactionMessage any
	if evt.Message.ReactionMessage != nil {
		reactionMessage = *evt.Message.ReactionMessage.Text
	}

	body := map[string]interface{}{
		"audio":            audioMedia,
		"contact":          evt.Message.GetContactMessage(),
		"document":         documentMedia,
		"forwarded":        forwarded,
		"from":             evt.Info.SourceString(),
		"image":            imageMedia,
		"list":             evt.Message.GetListMessage(),
		"live_location":    evt.Message.GetLiveLocationMessage(),
		"location":         evt.Message.GetLocationMessage(),
		"message":          message,
		"message_id":       evt.Info.ID,
		"order":            evt.Message.GetOrderMessage(),
		"pushname":         evt.Info.PushName,
		"quoted_message":   quotedmessage,
		"reaction_message": reactionMessage,
		"sticker":          stickerMedia,
		"video":            videoMedia,
		"view_once":        evt.Message.GetViewOnceMessage(),
	}

	if imageMedia != nil {
		path, err := DownloadMedia(config.PathMedia, imageMedia)
		if err != nil {
			return pkgError.WebhookError(fmt.Sprintf("Failed to download image: %v", err))
		}
		body["image"] = path
	}
	if stickerMedia != nil {
		path, err := DownloadMedia(config.PathMedia, stickerMedia)
		if err != nil {
			return pkgError.WebhookError(fmt.Sprintf("Failed to download sticker: %v", err))
		}
		body["sticker"] = path
	}
	if videoMedia != nil {
		path, err := DownloadMedia(config.PathMedia, videoMedia)
		if err != nil {
			return pkgError.WebhookError(fmt.Sprintf("Failed to download video: %v", err))
		}
		body["video"] = path
	}
	if audioMedia != nil {
		path, err := DownloadMedia(config.PathMedia, audioMedia)
		if err != nil {
			return pkgError.WebhookError(fmt.Sprintf("Failed to download audio: %v", err))
		}
		body["audio"] = path
	}
	if documentMedia != nil {
		path, err := DownloadMedia(config.PathMedia, documentMedia)
		if err != nil {
			return pkgError.WebhookError(fmt.Sprintf("Failed to download document: %v", err))
		}
		body["document"] = path
	}

	postBody, err := json.Marshal(body)
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("Failed to marshal body: %v", err))
	}

	req, err := http.NewRequest(http.MethodPost, config.WhatsappAutoReplyWebhook, bytes.NewBuffer(postBody))
	if err != nil {
		return pkgError.WebhookError(fmt.Sprintf("error when create http object %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	if _, err = client.Do(req); err != nil {
		return pkgError.WebhookError(fmt.Sprintf("error when submit webhook %v", err))
	}
	return nil
}

func isGroupJid(jid string) bool {
	return strings.Contains(jid, "@g.us")
}

func DownloadMedia(storageLocation string, mediaFile whatsmeow.DownloadableMessage) (path string, err error) {
	if mediaFile == nil {
		logrus.Info("Skip download because data is nil")
		return "", nil
	}

	data, err := cli.Download(mediaFile)
	if err != nil {
		return path, err
	}

	var mimeType string
	switch media := mediaFile.(type) {
	case *waProto.ImageMessage:
		mimeType = media.GetMimetype()
	case *waProto.AudioMessage:
		mimeType = media.GetMimetype()
	case *waProto.VideoMessage:
		mimeType = media.GetMimetype()
	case *waProto.StickerMessage:
		mimeType = media.GetMimetype()
	case *waProto.DocumentMessage:
		mimeType = media.GetMimetype()
	}

	extensions, _ := mime.ExtensionsByType(mimeType)
	path = fmt.Sprintf("%s/%d-%s%s", storageLocation, time.Now().Unix(), uuid.NewString(), extensions[0])
	err = os.WriteFile(path, data, 0600)
	if err != nil {
		return path, err
	}
	return path, nil
}
