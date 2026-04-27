package utils

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"go.mau.fi/whatsmeow"
)

var knownDocumentMIMEByExtension = map[string]string{
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":  "application/vnd.ms-powerpoint",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
}

var knownDocumentExtensionByMIME map[string]string

func init() {
	knownDocumentExtensionByMIME = make(map[string]string, len(knownDocumentMIMEByExtension))
	for ext, mimeType := range knownDocumentMIMEByExtension {
		knownDocumentExtensionByMIME[strings.ToLower(mimeType)] = ext
	}
}

func resolveKnownDocumentMIME(ext string) (string, bool) {
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	mimeType, ok := knownDocumentMIMEByExtension[ext]
	return mimeType, ok
}

func resolveKnownDocumentExtension(mimeType string) (string, bool) {
	ext, ok := knownDocumentExtensionByMIME[strings.ToLower(mimeType)]
	return ext, ok
}

// KnownDocumentMIMEByExtension returns a known MIME type for a given Office document extension.
func KnownDocumentMIMEByExtension(ext string) (string, bool) {
	return resolveKnownDocumentMIME(ext)
}

// KnownDocumentExtensionByMIME returns a known Office document extension for a given MIME type.
func KnownDocumentExtensionByMIME(mimeType string) (string, bool) {
	return resolveKnownDocumentExtension(mimeType)
}

func determineMediaExtension(originalFilename, mimeType string) string {
	if originalFilename != "" {
		if ext := filepath.Ext(originalFilename); ext != "" {
			return ext
		}
	}

	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	if ext, ok := resolveKnownDocumentExtension(mimeType); ok {
		return ext
	}

	if ext, err := mime.ExtensionsByType(mimeType); err == nil && len(ext) > 0 {
		return ext[0]
	}

	if parts := strings.Split(mimeType, "/"); len(parts) > 1 {
		return "." + parts[len(parts)-1]
	}

	return ""
}

// ExtractMessageTextFromProto extracts text content from a WhatsApp proto message
func ExtractMessageTextFromProto(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}

	// Check for regular text message
	if text := msg.GetConversation(); text != "" {
		return text
	}

	// Check for extended text message (with link preview, etc.)
	if extendedText := msg.GetExtendedTextMessage(); extendedText != nil {
		return extendedText.GetText()
	}

	// Check for image with caption
	if img := msg.GetImageMessage(); img != nil && img.GetCaption() != "" {
		return img.GetCaption()
	}

	// Check for video with caption
	if vid := msg.GetVideoMessage(); vid != nil && vid.GetCaption() != "" {
		return vid.GetCaption()
	}

	// Check for document with caption
	if doc := msg.GetDocumentMessage(); doc != nil && doc.GetCaption() != "" {
		return doc.GetCaption()
	}

	// Check for buttons response message
	if buttonsResponse := msg.GetButtonsResponseMessage(); buttonsResponse != nil {
		return buttonsResponse.GetSelectedDisplayText()
	}

	// Check for list response message
	if listResponse := msg.GetListResponseMessage(); listResponse != nil {
		return listResponse.GetTitle()
	}

	// Check for template button reply
	if templateButtonReply := msg.GetTemplateButtonReplyMessage(); templateButtonReply != nil {
		return templateButtonReply.GetSelectedDisplayText()
	}

	return ""
}

// ExtractMediaCaption extracts caption text from media messages (image, video, document, PTV).
func ExtractMediaCaption(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if img := msg.GetImageMessage(); img != nil {
		return img.GetCaption()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return vid.GetCaption()
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		return doc.GetCaption()
	}
	if ptv := msg.GetPtvMessage(); ptv != nil {
		return ptv.GetCaption()
	}
	return ""
}

// ExtractMessageTextFromEvent extracts text content from a WhatsApp event message with emojis
func ExtractMessageTextFromEvent(evt *events.Message) string {
	messageText := evt.Message.GetConversation()
	if extendedText := evt.Message.GetExtendedTextMessage(); extendedText != nil {
		messageText = extendedText.GetText()
	} else if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
			if extendedText := editedMessage.GetExtendedTextMessage(); extendedText != nil {
				messageText = extendedText.GetText()
			}
		}
	} else if imageMessage := evt.Message.GetImageMessage(); imageMessage != nil {
		messageText = imageMessage.GetCaption()
		if messageText == "" {
			messageText = "🖼️ Image"
		}
	} else if documentMessage := evt.Message.GetDocumentMessage(); documentMessage != nil {
		messageText = documentMessage.GetCaption()
		if messageText == "" {
			fileName := strings.TrimSpace(documentMessage.GetFileName())
			if fileName != "" {
				messageText = "📄 " + fileName
			} else {
				messageText = "📄 Document"
			}
		}
	} else if videoMessage := evt.Message.GetVideoMessage(); videoMessage != nil {
		messageText = videoMessage.GetCaption()
		if messageText == "" {
			messageText = "🎥 Video"
		}
	} else if liveLocationMessage := evt.Message.GetLiveLocationMessage(); liveLocationMessage != nil {
		messageText = liveLocationMessage.GetCaption()
		if messageText == "" {
			messageText = "📍 Live Location"
		}
	} else if locationMessage := evt.Message.GetLocationMessage(); locationMessage != nil {
		messageText = locationMessage.GetName()
		if messageText == "" {
			messageText = "📍 Location"
		}
	} else if contactMessage := evt.Message.GetContactMessage(); contactMessage != nil {
		messageText = contactMessage.GetDisplayName()
		if messageText == "" {
			messageText = "👤 Contact"
		}
	} else if listMessage := evt.Message.GetListMessage(); listMessage != nil {
		messageText = listMessage.GetTitle()
		if messageText == "" {
			messageText = "📝 List"
		}
	} else if orderMessage := evt.Message.GetOrderMessage(); orderMessage != nil {
		messageText = orderMessage.GetOrderTitle()
		if messageText == "" {
			messageText = "🛍️ Order"
		}
	} else if paymentMessage := evt.Message.GetPaymentInviteMessage(); paymentMessage != nil {
		messageText = paymentMessage.GetServiceType().String()
		if messageText == "" {
			messageText = "💳 Payment"
		}
	} else if audioMessage := evt.Message.GetAudioMessage(); audioMessage != nil {
		messageText = "🎧 Audio"
		if audioMessage.GetPTT() {
			messageText = "🎤 Voice Message"
		}
	} else if pollMessageV3 := evt.Message.GetPollCreationMessageV3(); pollMessageV3 != nil {
		messageText = pollMessageV3.GetName()
		if messageText == "" {
			messageText = "📊 Poll"
		}
	} else if pollMessageV4 := evt.Message.GetPollCreationMessageV4(); pollMessageV4 != nil {
		if pollMessage := pollMessageV4.GetMessage(); pollMessage != nil {
			messageText = pollMessage.GetConversation()
		}
		if messageText == "" {
			messageText = "📊 Poll"
		}
	} else if pollMessageV5 := evt.Message.GetPollCreationMessageV5(); pollMessageV5 != nil {
		messageText = pollMessageV5.GetName()
		if messageText == "" {
			messageText = "📊 Poll"
		}
	}
	return messageText
}

// ExtractMediaInfo extracts media information from a WhatsApp message
func ExtractMediaInfo(msg *waE2E.Message) (mediaType string, filename string, url string, mediaKey []byte, fileSHA256 []byte, fileEncSHA256 []byte, fileLength uint64) {
	if msg == nil {
		return "", "", "", nil, nil, nil, 0
	}

	// Check for image message
	if img := msg.GetImageMessage(); img != nil {
		filename = GenerateMediaFilename("image", "jpg", img.GetCaption())
		return "image", filename,
			img.GetURL(), img.GetMediaKey(), img.GetFileSHA256(),
			img.GetFileEncSHA256(), img.GetFileLength()
	}

	// Check for video message
	if vid := msg.GetVideoMessage(); vid != nil {
		filename = GenerateMediaFilename("video", "mp4", vid.GetCaption())
		return "video", filename,
			vid.GetURL(), vid.GetMediaKey(), vid.GetFileSHA256(),
			vid.GetFileEncSHA256(), vid.GetFileLength()
	}

	// Check for PTV (video note) message - circular video messages
	if ptv := msg.GetPtvMessage(); ptv != nil {
		filename = GenerateMediaFilename("video_note", "mp4", ptv.GetCaption())
		return "video_note", filename,
			ptv.GetURL(), ptv.GetMediaKey(), ptv.GetFileSHA256(),
			ptv.GetFileEncSHA256(), ptv.GetFileLength()
	}

	// Check for audio message
	if aud := msg.GetAudioMessage(); aud != nil {
		extension := "ogg"
		if aud.GetPTT() {
			extension = "ogg" // Voice notes are typically ogg
		}
		filename = GenerateMediaFilename("audio", extension, "")
		return "audio", filename,
			aud.GetURL(), aud.GetMediaKey(), aud.GetFileSHA256(),
			aud.GetFileEncSHA256(), aud.GetFileLength()
	}

	// Check for document message
	if doc := msg.GetDocumentMessage(); doc != nil {
		filename = doc.GetFileName()
		if filename == "" {
			filename = GenerateMediaFilename("document", "", doc.GetTitle())
		}
		return "document", filename,
			doc.GetURL(), doc.GetMediaKey(), doc.GetFileSHA256(),
			doc.GetFileEncSHA256(), doc.GetFileLength()
	}

	// Check for sticker message
	if sticker := msg.GetStickerMessage(); sticker != nil {
		filename = GenerateMediaFilename("sticker", "webp", "")
		return "sticker", filename,
			sticker.GetURL(), sticker.GetMediaKey(), sticker.GetFileSHA256(),
			sticker.GetFileEncSHA256(), sticker.GetFileLength()
	}

	return "", "", "", nil, nil, nil, 0
}

// ExtractContextInfo returns the ContextInfo from whichever message sub-type
// is present. Returns nil when the message has no ContextInfo.
func ExtractContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	switch {
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	case msg.GetContactMessage() != nil:
		return msg.GetContactMessage().GetContextInfo()
	case msg.GetLocationMessage() != nil:
		return msg.GetLocationMessage().GetContextInfo()
	case msg.GetPtvMessage() != nil:
		return msg.GetPtvMessage().GetContextInfo()
	case msg.GetLiveLocationMessage() != nil:
		return msg.GetLiveLocationMessage().GetContextInfo()
	}
	return nil
}

// ExtractEphemeralExpiration extracts ephemeral expiration from a WhatsApp message
func ExtractEphemeralExpiration(msg *waE2E.Message) uint32 {
	if msg == nil {
		return 0
	}

	if ci := ExtractContextInfo(msg); ci != nil {
		if exp := ci.GetExpiration(); exp != 0 {
			return exp
		}
	}

	if pm := msg.GetProtocolMessage(); pm != nil {
		if exp := pm.GetEphemeralExpiration(); exp != 0 {
			return exp
		}
	}

	return 0
}

var reNonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)

// GenerateMediaFilename creates a filename for media files
func GenerateMediaFilename(mediaType, extension, caption string) string {
	timestamp := time.Now().Format("20060102_150405")
	name := mediaType + "_" + timestamp

	if caption != "" {
		cleanCaption := reNonAlphanumeric.ReplaceAllString(caption, "_")
		if len(cleanCaption) > 30 {
			cleanCaption = cleanCaption[:30]
		}
		name += "_" + cleanCaption
	}

	if extension != "" {
		name += "." + extension
	}
	return name
}

var reDigits = regexp.MustCompile(`\d+`)

// ExtractPhoneNumber is a helper function to extract the phone number from a JID
func ExtractPhoneNumber(jid string) string {
	matches := reDigits.FindAllString(jid, -1)
	// The first match should be the phone number
	if len(matches) > 0 {
		return matches[0]
	}
	// If no matches are found, return an empty string
	return ""
}

// IsGroupJID is a helper function to check if the JID is from a group
func IsGroupJID(jid string) bool {
	return strings.Contains(jid, "@g.us")
}

// GetPlatformName returns the platform name based on device ID
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

// ParseJID parses a string into a JID
func ParseJID(arg string) (types.JID, error) {
	if len(arg) > 0 && arg[0] == '+' {
		arg = arg[1:]
	}
	if !strings.ContainsRune(arg, '@') {
		return types.NewJID(arg, types.DefaultUserServer), nil
	}

	recipient, err := types.ParseJID(arg)
	if err != nil {
		return recipient, fmt.Errorf("invalid JID %s: %v", arg, err)
	}

	if recipient.User == "" {
		return recipient, fmt.Errorf("invalid JID %v: no server specified", arg)
	}
	return recipient, nil
}

// FormatJID formats a JID string by removing any :number suffix
func FormatJID(jid string) types.JID {
	// Remove any :number suffix if present
	if idx := strings.LastIndex(jid, ":"); idx != -1 && strings.Contains(jid, "@s.whatsapp.net") {
		jid = jid[:idx] + jid[strings.Index(jid, "@s.whatsapp.net"):]
	}
	formattedJID, err := ParseJID(jid)
	if err != nil {
		return types.JID{}
	}
	return formattedJID
}

// ExtractedMedia represents extracted media information
type ExtractedMedia struct {
	MediaPath string `json:"media_path"`
	MimeType  string `json:"mime_type"`
	Caption   string `json:"caption"`
}

// ExtractMedia is a helper function to extract media from whatsapp
func ExtractMedia(ctx context.Context, client *whatsmeow.Client, storageLocation string, mediaFile whatsmeow.DownloadableMessage) (extractedMedia ExtractedMedia, err error) {
	if mediaFile == nil {
		logrus.Info("Skip download because data is nil")
		return extractedMedia, nil
	}

	data, err := client.Download(ctx, mediaFile)
	if err != nil {
		return extractedMedia, err
	}

	// Validate file size before writing to disk
	maxFileSize := config.WhatsappSettingMaxDownloadSize
	if int64(len(data)) > maxFileSize {
		return extractedMedia, fmt.Errorf("file size exceeds the maximum limit of %d bytes", maxFileSize)
	}

	var originalFilename string

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
		originalFilename = media.GetFileName()
	}

	extension := determineMediaExtension(originalFilename, extractedMedia.MimeType)

	extractedMedia.MediaPath = fmt.Sprintf("%s/%d-%s%s", storageLocation, time.Now().Unix(), uuid.NewString(), extension)
	err = os.WriteFile(extractedMedia.MediaPath, data, 0600)
	if err != nil {
		return extractedMedia, err
	}
	return extractedMedia, nil
}

// SanitizePhone sanitizes phone number by adding appropriate WhatsApp suffix
const maxPhoneNumberLength = 15 // Maximum digits in a phone number

func SanitizePhone(phone *string) {
	if phone != nil && len(*phone) > 0 && !strings.Contains(*phone, "@") {
		if len(*phone) <= maxPhoneNumberLength {
			*phone = fmt.Sprintf("%s%s", *phone, config.WhatsappTypeUser)
		} else {
			*phone = fmt.Sprintf("%s%s", *phone, config.WhatsappTypeGroup)
		}
	}
}

// IsOnWhatsapp checks if a number is registered on WhatsApp
func IsOnWhatsapp(client *whatsmeow.Client, jid string) bool {
	// only check if the jid is a user with @s.whatsapp.net
	if strings.Contains(jid, "@s.whatsapp.net") {
		// Extract phone number from JID and add + prefix for international format
		phone := strings.TrimSuffix(jid, "@s.whatsapp.net")
		if phone == "" {
			return false
		}

		// whatsmeow expects international format with + prefix
		if !strings.HasPrefix(phone, "+") {
			phone = "+" + phone
		}

		// Add timeout to prevent indefinite blocking
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		data, err := client.IsOnWhatsApp(ctx, []string{phone})
		if err != nil {
			logrus.Error("Failed to check if user is on whatsapp: ", err)
			return false
		}

		// Empty response means number not found/invalid
		if len(data) == 0 {
			return false
		}

		// Check if any result indicates the number is NOT on WhatsApp
		for _, v := range data {
			if !v.IsIn {
				return false
			}
		}

		return true
	}

	// For non-user JIDs (groups, newsletters), skip validation
	return true
}

// ValidateJidWithLogin validates JID with login check
func ValidateJidWithLogin(client *whatsmeow.Client, jid string) (types.JID, error) {
	MustLogin(client)

	parsedJID, err := ParseJID(jid)
	if err != nil {
		return types.JID{}, err
	}

	// If it's an @lid JID, try to resolve to phone number
	if parsedJID.Server == "lid" {
		resolved := ResolveLIDToPhone(context.Background(), parsedJID, client)
		if resolved.Server != "lid" {
			parsedJID = resolved // Use resolved phone-based JID
		}
		// Skip IsOnWhatsapp check for LIDs
		return parsedJID, nil
	}

	if config.WhatsappAccountValidation && !IsOnWhatsapp(client, jid) {
		return types.JID{}, pkgError.InvalidJID(fmt.Sprintf("Phone %s is not on whatsapp", jid))
	}

	return parsedJID, nil
}

// MustLogin ensures the WhatsApp client is logged in
func MustLogin(client *whatsmeow.Client) {
	if client == nil {
		panic(pkgError.InternalServerError("Whatsapp client is not initialized"))
	}
	if !client.IsConnected() {
		panic(pkgError.ErrNotConnected)
	} else if !client.IsLoggedIn() {
		panic(pkgError.ErrNotLoggedIn)
	}
}

// ResolveLIDToPhone converts @lid JIDs to their corresponding @s.whatsapp.net JIDs
// Returns the original JID if it's not an @lid or if LID lookup fails
func ResolveLIDToPhone(ctx context.Context, jid types.JID, client *whatsmeow.Client) types.JID {
	// Only process @lid JIDs
	if jid.Server != "lid" {
		return jid
	}

	// Safety check
	if client == nil || client.Store == nil || client.Store.LIDs == nil {
		logrus.Warnf("Cannot resolve LID %s: client not available", jid.String())
		return jid
	}

	// Attempt to get the phone number for this LID
	pn, err := client.Store.LIDs.GetPNForLID(ctx, jid)
	if err != nil {
		logrus.Debugf("Failed to resolve LID %s to phone number: %v", jid.String(), err)
		return jid
	}

	// If we got a valid phone number, use it
	if !pn.IsEmpty() {
		logrus.Debugf("Resolved LID %s to phone number %s", jid.String(), pn.String())
		return pn
	}

	// Fallback to original JID
	return jid
}

// ResolvePhoneToLID converts @s.whatsapp.net JIDs to their corresponding @lid JIDs
// Returns empty JID if it's not a user JID or if LID lookup fails
func ResolvePhoneToLID(ctx context.Context, jid types.JID, client *whatsmeow.Client) types.JID {
	// Only process user JIDs
	if jid.Server != types.DefaultUserServer {
		return types.JID{}
	}

	// Safety check
	if client == nil || client.Store == nil || client.Store.LIDs == nil {
		logrus.Debugf("Cannot resolve phone %s to LID: client not available", jid.String())
		return types.JID{}
	}

	// Attempt to get the LID for this phone number
	lid, err := client.Store.LIDs.GetLIDForPN(ctx, jid)
	if err != nil {
		logrus.Debugf("Failed to resolve phone %s to LID: %v", jid.String(), err)
		return types.JID{}
	}

	// If we got a valid LID, return it
	if !lid.IsEmpty() {
		logrus.Debugf("Resolved phone %s to LID %s", jid.String(), lid.String())
		return lid
	}

	return types.JID{}
}

// Internal message types for event handling
type EvtMessage struct {
	Text          string `json:"text"`
	ID            string `json:"id"`
	RepliedId     string `json:"replied_id"`
	QuotedMessage string `json:"quoted_message"`
}

type EvtReaction struct {
	Message string `json:"message"`
	ID      string `json:"id"`
}

// GetMessageDigestOrSignature generates HMAC signature for message
func GetMessageDigestOrSignature(msg, key []byte) (string, error) {
	mac := hmac.New(sha256.New, key)
	_, err := mac.Write(msg)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// UnwrapMessage unwraps FutureProof wrappers (ephemeral, view-once, etc.)
// to access the inner message content. WhatsApp wraps messages in these
// containers when disappearing messages or view-once is enabled.
// The original message is not modified; the unwrapped inner message is returned.
func UnwrapMessage(msg *waE2E.Message) *waE2E.Message {
	if msg == nil {
		return msg
	}
	inner := msg
	for i := 0; i < 3; i++ { // safeguard against excessively nested wrappers
		if vm := inner.GetViewOnceMessage(); vm != nil && vm.GetMessage() != nil {
			inner = vm.GetMessage()
			continue
		}
		if em := inner.GetEphemeralMessage(); em != nil && em.GetMessage() != nil {
			inner = em.GetMessage()
			continue
		}
		if vm2 := inner.GetViewOnceMessageV2(); vm2 != nil && vm2.GetMessage() != nil {
			inner = vm2.GetMessage()
			continue
		}
		if vm2e := inner.GetViewOnceMessageV2Extension(); vm2e != nil && vm2e.GetMessage() != nil {
			inner = vm2e.GetMessage()
			continue
		}
		break
	}
	return inner
}

// BuildEventMessage builds event message structure
func BuildEventMessage(evt *events.Message) (message EvtMessage) {
	msg := UnwrapMessage(evt.Message)

	message.Text = msg.GetConversation()
	message.ID = evt.Info.ID

	if extendedMessage := msg.GetExtendedTextMessage(); extendedMessage != nil {
		message.Text = extendedMessage.GetText()
	} else if protocolMessage := msg.GetProtocolMessage(); protocolMessage != nil {
		if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
			if extendedText := editedMessage.GetExtendedTextMessage(); extendedText != nil {
				message.Text = extendedText.GetText()
			}
			if ci := ExtractContextInfo(editedMessage); ci != nil {
				message.RepliedId = ci.GetStanzaID()
				message.QuotedMessage = ci.GetQuotedMessage().GetConversation()
			}
			return message
		}
	}

	if ci := ExtractContextInfo(msg); ci != nil {
		message.RepliedId = ci.GetStanzaID()
		message.QuotedMessage = ci.GetQuotedMessage().GetConversation()
	}

	return message
}

func BuildEventReaction(evt *events.Message) (waReaction EvtReaction) {
	msg := UnwrapMessage(evt.Message)
	if reactionMessage := msg.GetReactionMessage(); reactionMessage != nil {
		waReaction.Message = reactionMessage.GetText()
		waReaction.ID = reactionMessage.GetKey().GetID()
	}
	return waReaction
}

func BuildForwarded(evt *events.Message) bool {
	msg := UnwrapMessage(evt.Message)
	if ci := ExtractContextInfo(msg); ci != nil {
		return ci.GetIsForwarded()
	}
	if pm := msg.GetProtocolMessage(); pm != nil {
		if edited := pm.GetEditedMessage(); edited != nil {
			if ci := ExtractContextInfo(edited); ci != nil {
				return ci.GetIsForwarded()
			}
		}
	}
	return false
}

// ExtractExternalAdReply extracts Meta Ads referral/attribution metadata from
// incoming Click-to-WhatsApp ad messages. Returns nil when no ad data is present.
func ExtractExternalAdReply(msg *waE2E.Message) map[string]any {
	if msg == nil {
		return nil
	}

	ci := ExtractContextInfo(UnwrapMessage(msg))
	if ci == nil {
		return nil
	}

	ad := ci.GetExternalAdReply()
	if ad == nil {
		return nil
	}

	referral := make(map[string]any)

	if v := ad.GetCtwaClid(); v != "" {
		referral["ctwa_clid"] = v
	}
	if v := ad.GetSourceURL(); v != "" {
		referral["source_url"] = v
	}
	if v := ad.GetSourceID(); v != "" {
		referral["source_id"] = v
	}
	if v := ad.GetRef(); v != "" {
		referral["ref"] = v
	}
	if v := ad.GetSourceApp(); v != "" {
		referral["source_app"] = v
	}
	if v := ad.GetTitle(); v != "" {
		referral["ad_title"] = v
	}
	if v := ad.GetBody(); v != "" {
		referral["ad_body"] = v
	}
	if v := ad.GetThumbnailURL(); v != "" {
		referral["thumbnail_url"] = v
	}
	if v := ad.GetOriginalImageURL(); v != "" {
		referral["original_image_url"] = v
	}
	if v := ad.GetMediaURL(); v != "" {
		referral["media_url"] = v
	}
	if ad.MediaType != nil {
		referral["media_type"] = ad.GetMediaType().String()
	}
	if ad.ShowAdAttribution != nil {
		referral["show_ad_attribution"] = ad.GetShowAdAttribution()
	}
	if ad.ContainsAutoReply != nil {
		referral["contains_auto_reply"] = ad.GetContainsAutoReply()
	}
	if ad.AutomatedGreetingMessageShown != nil {
		referral["automated_greeting_message_shown"] = ad.GetAutomatedGreetingMessageShown()
	}
	if v := ad.GetGreetingMessageBody(); v != "" {
		referral["greeting_message_body"] = v
	}
	if ad.ClickToWhatsappCall != nil {
		referral["click_to_whatsapp_call"] = ad.GetClickToWhatsappCall()
	}
	if v := ad.GetSourceType(); v != "" {
		referral["source_type"] = v
	}
	if ad.AdType != nil {
		referral["ad_type"] = ad.GetAdType().String()
	}

	if len(referral) == 0 {
		return nil
	}

	return referral
}
