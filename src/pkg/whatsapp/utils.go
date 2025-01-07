package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/proto/waE2E"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// ExtractMedia is a helper function to extract media from whatsapp
func ExtractMedia(storageLocation string, mediaFile whatsmeow.DownloadableMessage) (extractedMedia ExtractedMedia, err error) {
	if mediaFile == nil {
		logrus.Info("Skip download because data is nil")
		return extractedMedia, nil
	}

	data, err := cli.Download(mediaFile)
	if err != nil {
		return extractedMedia, err
	}

	// Validate file size before writing to disk
	maxFileSize := config.WhatsappSettingMaxDownloadSize
	if int64(len(data)) > maxFileSize {
		return extractedMedia, fmt.Errorf("file size exceeds the maximum limit of %d bytes", maxFileSize)
	}

	switch media := mediaFile.(type) {
	case *waE2E.ImageMessage:
		extractedMedia.MimeType = media.GetMimetype()
		extractedMedia.Caption = media.GetCaption()
	case *waE2E.AudioMessage:
		extractedMedia.MimeType = media.GetMimetype()
	case *waE2E.VideoMessage:
		extractedMedia.MimeType = media.GetMimetype()
		extractedMedia.Caption = media.GetCaption()
	case *waE2E.StickerMessage:
		extractedMedia.MimeType = media.GetMimetype()
	case *waE2E.DocumentMessage:
		extractedMedia.MimeType = media.GetMimetype()
		extractedMedia.Caption = media.GetCaption()
	}

	var extension string
	if ext, err := mime.ExtensionsByType(extractedMedia.MimeType); err == nil && len(ext) > 0 {
		extension = ext[0]
	} else if parts := strings.Split(extractedMedia.MimeType, "/"); len(parts) > 1 {
		extension = "." + parts[len(parts)-1]
	}

	extractedMedia.MediaPath = fmt.Sprintf("%s/%d-%s%s", storageLocation, time.Now().Unix(), uuid.NewString(), extension)
	err = os.WriteFile(extractedMedia.MediaPath, data, 0600)
	if err != nil {
		return extractedMedia, err
	}
	return extractedMedia, nil
}

func SanitizePhone(phone *string) {
	if phone != nil && len(*phone) > 0 && !strings.Contains(*phone, "@") {
		if len(*phone) <= 15 {
			*phone = fmt.Sprintf("%s%s", *phone, config.WhatsappTypeUser)
		} else {
			*phone = fmt.Sprintf("%s%s", *phone, config.WhatsappTypeGroup)
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
	}

	recipient, err := types.ParseJID(arg)
	if err != nil {
		fmt.Printf("invalid JID %s: %v", arg, err)
		return recipient, pkgError.ErrInvalidJID
	}

	if recipient.User == "" {
		fmt.Printf("invalid JID %v: no server specified", arg)
		return recipient, pkgError.ErrInvalidJID
	}
	return recipient, nil
}

func IsOnWhatsapp(waCli *whatsmeow.Client, jid string) bool {
	// only check if the jid a user with @s.whatsapp.net
	if strings.Contains(jid, "@s.whatsapp.net") {
		data, err := waCli.IsOnWhatsApp([]string{jid})
		if err != nil {
			logrus.Error("Failed to check if user is on whatsapp: ", err)
			return false
		}

		for _, v := range data {
			if !v.IsIn {
				return false
			}
		}
	}

	return true
}

func ValidateJidWithLogin(waCli *whatsmeow.Client, jid string) (types.JID, error) {
	MustLogin(waCli)

	if config.WhatsappAccountValidation && !IsOnWhatsapp(waCli, jid) {
		return types.JID{}, pkgError.InvalidJID(fmt.Sprintf("Phone %s is not on whatsapp", jid))
	}

	return ParseJID(jid)
}

func MustLogin(waCli *whatsmeow.Client) {
	if waCli == nil {
		panic(pkgError.InternalServerError("Whatsapp client is not initialized"))
	}
	if !waCli.IsConnected() {
		panic(pkgError.ErrNotConnected)
	} else if !waCli.IsLoggedIn() {
		panic(pkgError.ErrNotLoggedIn)
	}
}

// isGroupJid is a helper function to check if the message is from group
func isGroupJid(jid string) bool {
	return strings.Contains(jid, "@g.us")
}

// isFromMySelf is a helper function to check if the message is from my self (logged in account)
func isFromMySelf(jid string) bool {
	return extractPhoneNumber(jid) == extractPhoneNumber(cli.Store.ID.String())
}

// extractPhoneNumber is a helper function to extract the phone number from a JID
func extractPhoneNumber(jid string) string {
	regex := regexp.MustCompile(`\d+`)
	// Find all matches of the pattern in the JID
	matches := regex.FindAllString(jid, -1)
	// The first match should be the phone number
	if len(matches) > 0 {
		return matches[0]
	}
	// If no matches are found, return an empty string
	return ""
}

func getMessageDigestOrSignature(msg, key []byte) (string, error) {
	mac := hmac.New(sha256.New, key)
	_, err := mac.Write(msg)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func buildEventMessage(evt *events.Message) (message evtMessage) {
	message.Text = evt.Message.GetConversation()
	message.ID = evt.Info.ID

	if extendedMessage := evt.Message.GetExtendedTextMessage(); extendedMessage != nil {
		message.Text = extendedMessage.GetText()
		message.RepliedId = extendedMessage.ContextInfo.GetStanzaID()
		message.QuotedMessage = extendedMessage.ContextInfo.GetQuotedMessage().GetConversation()
	} else if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
			if extendedText := editedMessage.GetExtendedTextMessage(); extendedText != nil {
				message.Text = extendedText.GetText()
				message.RepliedId = extendedText.ContextInfo.GetStanzaID()
				message.QuotedMessage = extendedText.ContextInfo.GetQuotedMessage().GetConversation()
			}
		}
	}

	return message
}

func buildEventReaction(evt *events.Message) (waReaction evtReaction) {
	if reactionMessage := evt.Message.GetReactionMessage(); reactionMessage != nil {
		waReaction.Message = reactionMessage.GetText()
		waReaction.ID = reactionMessage.GetKey().GetID()
	}
	return waReaction
}

func buildForwarded(evt *events.Message) bool {
	if extendedText := evt.Message.GetExtendedTextMessage(); extendedText != nil {
		return extendedText.ContextInfo.GetIsForwarded()
	} else if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
			if extendedText := editedMessage.GetExtendedTextMessage(); extendedText != nil {
				return extendedText.ContextInfo.GetIsForwarded()
			}
		}
	}
	return false
}
