package utils

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/url"
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
	"google.golang.org/protobuf/proto"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
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

// ExtractPhoneFromVCard returns the first phone number found in a vCard's TEL field.
func ExtractPhoneFromVCard(vcard string) string {
	if vcard == "" {
		return ""
	}

	normalized := strings.ReplaceAll(vcard, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	var lines []string
	var current strings.Builder
	for _, rawLine := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(rawLine, " ") || strings.HasPrefix(rawLine, "\t") {
			if current.Len() > 0 {
				current.WriteString(line)
			}
			continue
		}
		if current.Len() > 0 {
			lines = append(lines, current.String())
			current.Reset()
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "TEL") {
			if idx := strings.LastIndex(line, ":"); idx >= 0 {
				return strings.TrimSpace(line[idx+1:])
			}
		}
	}
	return ""
}

// FormatLocationSummary builds a one-liner for an incoming location or live-location pin.
// The maps link is always included so content is never empty for coordinate-only pins.
// name and address are optional prefixes.
func FormatLocationSummary(name, address string, lat, long float64) string {
	var parts []string
	if n := strings.TrimSpace(name); n != "" {
		parts = append(parts, n)
	}
	if a := strings.TrimSpace(address); a != "" {
		parts = append(parts, a)
	}
	mapsLink := fmt.Sprintf("https://maps.google.com/?q=%g,%g", lat, long)
	parts = append(parts, mapsLink)
	return strings.Join(parts, " — ")
}

// FormatContactSummary builds a one-liner for a shared contact card.
// Pass plural=true for ContactsArrayMessage to use the "Contacts" prefix.
func FormatContactSummary(name, phone string, plural bool) string {
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)

	prefix := "Contact"
	if plural {
		prefix = "Contacts"
	}
	switch {
	case name != "" && phone != "":
		return fmt.Sprintf("%s: %s (%s)", prefix, name, phone)
	case name != "":
		return prefix + ": " + name
	case phone != "":
		return prefix + ": " + phone
	default:
		return prefix + " shared"
	}
}

// KnownDocumentMIMEByExtension returns a known MIME type for a given Office document extension.
func KnownDocumentMIMEByExtension(ext string) (string, bool) {
	return resolveKnownDocumentMIME(ext)
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
		if t := extendedText.GetText(); t != "" {
			return t
		}
		// Fall back to the matched URL text when the text field is empty
		// (e.g., pure link-preview messages with no accompanying caption).
		if m := extendedText.GetMatchedText(); m != "" {
			return m
		}
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

	// Check for shared contact card
	if contact := msg.GetContactMessage(); contact != nil {
		return FormatContactSummary(contact.GetDisplayName(), ExtractPhoneFromVCard(contact.GetVcard()), false)
	}

	// Check for shared multiple contact cards
	if contactsArray := msg.GetContactsArrayMessage(); contactsArray != nil {
		if contacts := contactsArray.GetContacts(); len(contacts) > 0 && contacts[0] != nil {
			first := contacts[0]
			return FormatContactSummary(first.GetDisplayName(), ExtractPhoneFromVCard(first.GetVcard()), true)
		}
		return "Contacts shared"
	}

	// Check for location pin
	if loc := msg.GetLocationMessage(); loc != nil {
		return FormatLocationSummary(loc.GetName(), loc.GetAddress(), loc.GetDegreesLatitude(), loc.GetDegreesLongitude())
	}

	// Check for live location
	if live := msg.GetLiveLocationMessage(); live != nil {
		return FormatLocationSummary(live.GetCaption(), "", live.GetDegreesLatitude(), live.GetDegreesLongitude())
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

// ExtractMediaInfo extracts media information from a WhatsApp message
func ExtractMediaInfo(msg *waE2E.Message) (mediaType string, filename string, mediaURL string, directPath string, mediaKey []byte, fileSHA256 []byte, fileEncSHA256 []byte, fileLength uint64) {
	if msg == nil {
		return "", "", "", "", nil, nil, nil, 0
	}

	// Check for image message
	if img := msg.GetImageMessage(); img != nil {
		filename = GenerateMediaFilename("image", "jpg", img.GetCaption())
		return "image", filename,
			img.GetURL(), img.GetDirectPath(), img.GetMediaKey(), img.GetFileSHA256(),
			img.GetFileEncSHA256(), img.GetFileLength()
	}

	// Check for video message
	if vid := msg.GetVideoMessage(); vid != nil {
		filename = GenerateMediaFilename("video", "mp4", vid.GetCaption())
		return "video", filename,
			vid.GetURL(), vid.GetDirectPath(), vid.GetMediaKey(), vid.GetFileSHA256(),
			vid.GetFileEncSHA256(), vid.GetFileLength()
	}

	// Check for PTV (video note) message - circular video messages
	if ptv := msg.GetPtvMessage(); ptv != nil {
		filename = GenerateMediaFilename("video_note", "mp4", ptv.GetCaption())
		return "video_note", filename,
			ptv.GetURL(), ptv.GetDirectPath(), ptv.GetMediaKey(), ptv.GetFileSHA256(),
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
			aud.GetURL(), aud.GetDirectPath(), aud.GetMediaKey(), aud.GetFileSHA256(),
			aud.GetFileEncSHA256(), aud.GetFileLength()
	}

	// Check for document message
	if doc := msg.GetDocumentMessage(); doc != nil {
		filename = doc.GetFileName()
		if filename == "" {
			filename = GenerateMediaFilename("document", "", doc.GetTitle())
		}
		return "document", filename,
			doc.GetURL(), doc.GetDirectPath(), doc.GetMediaKey(), doc.GetFileSHA256(),
			doc.GetFileEncSHA256(), doc.GetFileLength()
	}

	// Check for sticker message
	if sticker := msg.GetStickerMessage(); sticker != nil {
		filename = GenerateMediaFilename("sticker", "webp", "")
		return "sticker", filename,
			sticker.GetURL(), sticker.GetDirectPath(), sticker.GetMediaKey(), sticker.GetFileSHA256(),
			sticker.GetFileEncSHA256(), sticker.GetFileLength()
	}

	return "", "", "", "", nil, nil, nil, 0
}

// ResolveMediaDirectPath returns storedDirectPath, or derives a direct path
// from legacy rows that only persisted the full WhatsApp media URL.
func ResolveMediaDirectPath(storedDirectPath, mediaURL string) string {
	storedDirectPath = strings.TrimSpace(storedDirectPath)
	if storedDirectPath != "" {
		return storedDirectPath
	}

	mediaURL = strings.TrimSpace(mediaURL)
	if mediaURL == "" {
		return ""
	}

	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return ""
	}
	requestURI := parsed.RequestURI()
	if strings.HasPrefix(requestURI, "/") {
		return requestURI
	}
	return ""
}

// ErrUnsupportedForwardType is returned when a stored message cannot be rebuilt for forward-by-ID.
const ErrUnsupportedForwardType = "unsupported message type for forward"

// ForwardBuildOptions controls proto rebuild for forward-by-ID sends.
type ForwardBuildOptions struct {
	Duration *int
	Upload   *whatsmeow.UploadResponse
	MimeType string
}

var forwardableMediaTypes = map[string]struct{}{
	"image":      {},
	"video":      {},
	"video_note": {},
	"audio":      {},
	"ptt":        {},
	"document":   {},
	"sticker":    {},
}

// IsForwardableStorageMessage reports whether a stored row can be forwarded by ID.
func IsForwardableStorageMessage(message *domainChatStorage.Message) bool {
	if message == nil {
		return false
	}
	if message.MediaType == "call" {
		return false
	}
	if _, ok := forwardableMediaTypes[message.MediaType]; ok {
		return true
	}
	if message.MediaType != "" {
		return false
	}
	if strings.TrimSpace(message.Content) == "" {
		return false
	}
	return !isUnsupportedTextForwardContent(message.Content)
}

func isUnsupportedTextForwardContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if strings.HasPrefix(trimmed, "Contact:") || strings.HasPrefix(trimmed, "Contacts:") {
		return true
	}
	if strings.Contains(trimmed, "https://maps.google.com/?q=") {
		return true
	}
	return false
}

// IsForwardMediaMessage reports whether the stored row represents forwardable media (not plain text).
func IsForwardMediaMessage(message *domainChatStorage.Message) bool {
	if message == nil {
		return false
	}
	_, ok := forwardableMediaTypes[message.MediaType]
	return ok
}

func newForwardContextInfo(duration *int) *waE2E.ContextInfo {
	ci := &waE2E.ContextInfo{
		IsForwarded:     proto.Bool(true),
		ForwardingScore: proto.Uint32(100),
	}
	if duration != nil && *duration > 0 {
		ci.Expiration = proto.Uint32(uint32(*duration))
	}
	return ci
}

// defaultForwardMimeType returns a mime type for stored media whose original
// mime type was not persisted. WhatsApp transcodes media to fixed formats, so
// per-type defaults are accurate except for documents, which are derived from
// the stored filename.
func defaultForwardMimeType(message *domainChatStorage.Message) string {
	switch message.MediaType {
	case "image":
		return "image/jpeg"
	case "video", "video_note":
		return "video/mp4"
	case "ptt":
		return "audio/ogg; codecs=opus"
	case "audio":
		return "audio/mpeg"
	case "sticker":
		return "image/webp"
	case "document":
		ext := strings.ToLower(filepath.Ext(message.Filename))
		if mimeType, ok := knownDocumentMIMEByExtension[ext]; ok {
			return mimeType
		}
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
		return "application/octet-stream"
	default:
		return ""
	}
}

// BuildForwardMessageFromStorage rebuilds a sendable WhatsApp proto from chat storage.
func BuildForwardMessageFromStorage(message *domainChatStorage.Message, opts ForwardBuildOptions) (*waE2E.Message, error) {
	if message == nil {
		return nil, fmt.Errorf("message is nil")
	}
	if !IsForwardableStorageMessage(message) {
		return nil, errors.New(ErrUnsupportedForwardType)
	}

	contextInfo := newForwardContextInfo(opts.Duration)

	if message.MediaType == "" {
		return &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(message.Content),
				ContextInfo: contextInfo,
			},
		}, nil
	}

	var (
		mediaURL       string
		directPath     string
		mediaKey       []byte
		fileSHA256     []byte
		fileEncSHA256  []byte
		fileLength     uint64
	)

	if opts.Upload != nil {
		mediaURL = opts.Upload.URL
		directPath = opts.Upload.DirectPath
		mediaKey = opts.Upload.MediaKey
		fileSHA256 = opts.Upload.FileSHA256
		fileEncSHA256 = opts.Upload.FileEncSHA256
		fileLength = opts.Upload.FileLength
	} else {
		mediaURL = message.URL
		directPath = ResolveMediaDirectPath(message.DirectPath, message.URL)
		mediaKey = message.MediaKey
		fileSHA256 = message.FileSHA256
		fileEncSHA256 = message.FileEncSHA256
		fileLength = message.FileLength
		if directPath == "" && mediaURL == "" {
			return nil, fmt.Errorf("message %s has no media references", message.ID)
		}
	}

	caption := message.Content
	filename := message.Filename

	mimeType := opts.MimeType
	if mimeType == "" {
		mimeType = defaultForwardMimeType(message)
	}

	switch message.MediaType {
	case "image":
		img := &waE2E.ImageMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			Caption:       proto.String(caption),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			img.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{ImageMessage: img}, nil
	case "video":
		vid := &waE2E.VideoMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			Caption:       proto.String(caption),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			vid.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{VideoMessage: vid}, nil
	case "video_note":
		ptv := &waE2E.VideoMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			Caption:       proto.String(caption),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			ptv.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{PtvMessage: ptv}, nil
	case "audio", "ptt":
		aud := &waE2E.AudioMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			ContextInfo:   contextInfo,
		}
		if message.MediaType == "ptt" {
			aud.PTT = proto.Bool(true)
		}
		if mimeType != "" {
			aud.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{AudioMessage: aud}, nil
	case "document":
		doc := &waE2E.DocumentMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			FileName:      proto.String(filename),
			Caption:       proto.String(caption),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			doc.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{DocumentMessage: doc}, nil
	case "sticker":
		sticker := &waE2E.StickerMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			sticker.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{StickerMessage: sticker}, nil
	default:
		return nil, errors.New(ErrUnsupportedForwardType)
	}
}

// BuildDownloadableMessage reconstructs a whatsmeow downloadable media proto
// from stored chat media metadata.
func BuildDownloadableMessage(mediaType, mediaURL, directPath, filename string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) (whatsmeow.DownloadableMessage, error) {
	resolvedDirectPath := ResolveMediaDirectPath(directPath, mediaURL)

	switch mediaType {
	case "image":
		return &waE2E.ImageMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(resolvedDirectPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
		}, nil
	case "video", "video_note":
		return &waE2E.VideoMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(resolvedDirectPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
		}, nil
	case "audio", "ptt":
		return &waE2E.AudioMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(resolvedDirectPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
		}, nil
	case "document":
		return &waE2E.DocumentMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(resolvedDirectPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			FileName:      proto.String(filename),
		}, nil
	case "sticker":
		return &waE2E.StickerMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(resolvedDirectPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}
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
				message.QuotedMessage = ExtractMessageTextFromProto(ci.GetQuotedMessage())
			}
			return message
		}
	}

	if ci := ExtractContextInfo(msg); ci != nil {
		message.RepliedId = ci.GetStanzaID()
		message.QuotedMessage = ExtractMessageTextFromProto(ci.GetQuotedMessage())
	}

	return message
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
