package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/helpers"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/disintegration/imaging"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	"github.com/sirupsen/logrus"
	"github.com/valyala/fasthttp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

type serviceSend struct {
	appService      app.IAppUsecase
	chatStorageRepo domainChatStorage.IChatStorageRepository
}

func NewSendService(appService app.IAppUsecase, chatStorageRepo domainChatStorage.IChatStorageRepository) domainSend.ISendUsecase {
	return &serviceSend{
		appService:      appService,
		chatStorageRepo: chatStorageRepo,
	}
}

// wrapSendMessage wraps the message sending process with message ID saving
func (service serviceSend) wrapSendMessage(ctx context.Context, recipient types.JID, msg *waE2E.Message, content string) (whatsmeow.SendResponse, error) {
	ts, err := whatsapp.GetClient().SendMessage(ctx, recipient, msg)
	if err != nil {
		return whatsmeow.SendResponse{}, err
	}

	// Store the sent message using chatstorage
	senderJID := ""
	if whatsapp.GetClient().Store.ID != nil {
		senderJID = whatsapp.GetClient().Store.ID.String()
	}

	// Store message asynchronously with timeout
	// Use a goroutine to avoid blocking the send operation
	go func() {
		storeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := service.chatStorageRepo.StoreSentMessageWithContext(storeCtx, ts.ID, senderJID, recipient.String(), content, ts.Timestamp); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				logrus.Warn("Timeout storing sent message")
			} else {
				logrus.Warnf("Failed to store sent message: %v", err)
			}
		}
	}()

	return ts, nil
}

func (service serviceSend) SendText(ctx context.Context, request domainSend.MessageRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendMessage(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	// Create base message
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(request.Message),
			ContextInfo: &waE2E.ContextInfo{},
		},
	}

	// Add forwarding context if IsForwarded is true
	if request.BaseRequest.IsForwarded {
		msg.ExtendedTextMessage.ContextInfo.IsForwarded = proto.Bool(true)
		msg.ExtendedTextMessage.ContextInfo.ForwardingScore = proto.Uint32(100)
	}

	// Set disappearing message duration if provided
	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		msg.ExtendedTextMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	} else {
		msg.ExtendedTextMessage.ContextInfo.Expiration = proto.Uint32(service.getDefaultEphemeralExpiration(request.BaseRequest.Phone))
	}

	parsedMentions := service.getMentionFromText(ctx, request.Message)
	if len(parsedMentions) > 0 {
		msg.ExtendedTextMessage.ContextInfo.MentionedJID = parsedMentions
	}

	// Reply message
	if request.ReplyMessageID != nil && *request.ReplyMessageID != "" {
		message, err := service.chatStorageRepo.GetMessageByID(*request.ReplyMessageID)
		if err != nil {
			logrus.Warnf("Error retrieving reply message ID %s: %v, continuing without reply context", *request.ReplyMessageID, err)
		} else if message != nil { // Only set reply context if we found the message
			// Ensure we use a full JID (user@server) for the Participant field
			// Use the sender JID from storage as-is. Modern storage should already provide
			// fully-qualified JIDs (e.g., user@s.whatsapp.net or group@g.us). Avoid mutating
			// the JID here to prevent corrupting valid group or special JIDs.
			participantJID := message.Sender

			// Build base ContextInfo with reply details
			ctxInfo := &waE2E.ContextInfo{
				StanzaID:    request.ReplyMessageID,
				Participant: proto.String(participantJID),
				QuotedMessage: &waE2E.Message{
					Conversation: proto.String(message.Content),
				},
			}

			// Preserve forwarding flag if set
			if request.BaseRequest.IsForwarded {
				ctxInfo.IsForwarded = proto.Bool(true)
				ctxInfo.ForwardingScore = proto.Uint32(100)
			}

			// Preserve disappearing message duration if provided
			if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
				ctxInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
			} else {
				ctxInfo.Expiration = proto.Uint32(service.getDefaultEphemeralExpiration(participantJID))
			}

			// Preserve mentions
			if len(parsedMentions) > 0 {
				ctxInfo.MentionedJID = parsedMentions
			}

			msg.ExtendedTextMessage = &waE2E.ExtendedTextMessage{
				Text:        proto.String(request.Message),
				ContextInfo: ctxInfo,
			}
		} else {
			logrus.Warnf("Reply message ID %s not found in storage, continuing without reply context", *request.ReplyMessageID)
		}
	}

	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, request.Message)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Message sent to %s (server timestamp: %s)", request.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendImage(ctx context.Context, request domainSend.ImageRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendImage(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	var (
		imagePath      string
		imageThumbnail string
		imageName      string
		deletedItems   []string
		oriImagePath   string
	)

	if request.ImageURL != nil && *request.ImageURL != "" {
		// Download image from URL
		imageData, fileName, err := utils.DownloadImageFromURL(*request.ImageURL)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to download image from URL %v", err))
		}

		// Check if the downloaded image is WebP and convert to PNG if needed
		mimeType := http.DetectContentType(imageData)
		if mimeType == "image/webp" {
			// Convert WebP to PNG
			webpImage, err := imaging.Decode(bytes.NewReader(imageData))
			if err != nil {
				return response, pkgError.InternalServerError(fmt.Sprintf("failed to decode WebP image %v", err))
			}

			// Change file extension to PNG
			if strings.HasSuffix(strings.ToLower(fileName), ".webp") {
				fileName = fileName[:len(fileName)-5] + ".png"
			} else {
				fileName = fileName + ".png"
			}

			// Convert to PNG format
			var pngBuffer bytes.Buffer
			err = imaging.Encode(&pngBuffer, webpImage, imaging.PNG)
			if err != nil {
				return response, pkgError.InternalServerError(fmt.Sprintf("failed to convert WebP to PNG %v", err))
			}
			imageData = pngBuffer.Bytes()
		}

		oriImagePath = fmt.Sprintf("%s/%s", config.PathSendItems, fileName)
		imageName = fileName
		err = os.WriteFile(oriImagePath, imageData, 0644)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to save downloaded image %v", err))
		}
	} else if request.Image != nil {
		// Save image to server
		oriImagePath = fmt.Sprintf("%s/%s", config.PathSendItems, request.Image.Filename)
		err = fasthttp.SaveMultipartFile(request.Image, oriImagePath)
		if err != nil {
			return response, err
		}
		imageName = request.Image.Filename
	}
	deletedItems = append(deletedItems, oriImagePath)

	/* Generate thumbnail with smalled image size */
	srcImage, err := imaging.Open(oriImagePath)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("Failed to open image file '%s' for thumbnail generation: %v. Possible causes: file not found, unsupported format, or permission denied.", oriImagePath, err))
	}

	// Resize Thumbnail
	resizedImage := imaging.Resize(srcImage, 100, 0, imaging.Lanczos)
	imageThumbnail = fmt.Sprintf("%s/thumbnails-%s", config.PathSendItems, imageName)
	if err = imaging.Save(resizedImage, imageThumbnail); err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to save thumbnail %v", err))
	}
	deletedItems = append(deletedItems, imageThumbnail)

	if request.Compress {
		// Resize image
		openImageBuffer, err := imaging.Open(oriImagePath)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("Failed to open image file '%s' for compression: %v. Possible causes: file not found, unsupported format, or permission denied.", oriImagePath, err))
		}
		newImage := imaging.Resize(openImageBuffer, 600, 0, imaging.Lanczos)
		newImagePath := fmt.Sprintf("%s/new-%s", config.PathSendItems, imageName)
		if err = imaging.Save(newImage, newImagePath); err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to save image %v", err))
		}
		deletedItems = append(deletedItems, newImagePath)
		imagePath = newImagePath
	} else {
		imagePath = oriImagePath
	}

	// Send to WA server
	dataWaCaption := request.Caption
	dataWaImage, err := os.ReadFile(imagePath)
	if err != nil {
		return response, err
	}
	uploadedImage, err := service.uploadMedia(ctx, whatsmeow.MediaImage, dataWaImage, dataWaRecipient)
	if err != nil {
		fmt.Printf("failed to upload file: %v", err)
		return response, err
	}
	dataWaThumbnail, err := os.ReadFile(imageThumbnail)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to read thumbnail %v", err))
	}

	msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		JPEGThumbnail: dataWaThumbnail,
		Caption:       proto.String(dataWaCaption),
		URL:           proto.String(uploadedImage.URL),
		DirectPath:    proto.String(uploadedImage.DirectPath),
		MediaKey:      uploadedImage.MediaKey,
		Mimetype:      proto.String(http.DetectContentType(dataWaImage)),
		FileEncSHA256: uploadedImage.FileEncSHA256,
		FileSHA256:    uploadedImage.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dataWaImage))),
		ViewOnce:      proto.Bool(request.ViewOnce),
	}}

	if request.BaseRequest.IsForwarded {
		msg.ImageMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	// Set duration expiration
	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.ImageMessage.ContextInfo == nil {
			msg.ImageMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.ImageMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	caption := "🖼️ Image"
	if request.Caption != "" {
		caption = "🖼️ " + request.Caption
	}
	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, caption)
	go func() {
		errDelete := utils.RemoveFile(0, deletedItems...)
		if errDelete != nil {
			fmt.Println("error when deleting picture: ", errDelete)
		}
	}()
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Message sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendFile(ctx context.Context, request domainSend.FileRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendFile(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	fileBytes := helpers.MultipartFormFileHeaderToBytes(request.File)
	fileMimeType := resolveDocumentMIME(request.File.Filename, fileBytes)

	// Send to WA server
	uploadedFile, err := service.uploadMedia(ctx, whatsmeow.MediaDocument, fileBytes, dataWaRecipient)
	if err != nil {
		fmt.Printf("Failed to upload file: %v", err)
		return response, err
	}

	msg := &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		URL:           proto.String(uploadedFile.URL),
		Mimetype:      proto.String(fileMimeType),
		Title:         proto.String(request.File.Filename),
		FileSHA256:    uploadedFile.FileSHA256,
		FileLength:    proto.Uint64(uploadedFile.FileLength),
		MediaKey:      uploadedFile.MediaKey,
		FileName:      proto.String(request.File.Filename),
		FileEncSHA256: uploadedFile.FileEncSHA256,
		DirectPath:    proto.String(uploadedFile.DirectPath),
		Caption:       proto.String(request.Caption),
	}}

	if request.BaseRequest.IsForwarded {
		msg.DocumentMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.DocumentMessage.ContextInfo == nil {
			msg.DocumentMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.DocumentMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	caption := "📄 Document"
	if request.Caption != "" {
		caption = "📄 " + request.Caption
	}
	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, caption)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Document sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func resolveDocumentMIME(filename string, fileBytes []byte) string {
	extension := strings.ToLower(filepath.Ext(filename))
	if extension != "" {
		if mimeType, ok := utils.KnownDocumentMIMEByExtension(extension); ok {
			return mimeType
		}

		if mimeType := mime.TypeByExtension(extension); mimeType != "" {
			return mimeType
		}
	}

	return http.DetectContentType(fileBytes)
}

func (service serviceSend) SendVideo(ctx context.Context, request domainSend.VideoRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendVideo(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	var (
		videoPath      string
		videoThumbnail string
		deletedItems   []string
	)

	// Ensure temporary files are always removed, even on early returns
	defer func() {
		if len(deletedItems) > 0 {
			// Run cleanup in background with slight delay to avoid race with open handles
			go utils.RemoveFile(1, deletedItems...)
		}
	}()

	generateUUID := fiberUtils.UUIDv4()

	var oriVideoPath string

	// Determine source of video (URL or uploaded file)
	if request.VideoURL != nil && *request.VideoURL != "" {
		// Download video bytes
		videoBytes, fileName, errDownload := utils.DownloadVideoFromURL(*request.VideoURL)
		if errDownload != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to download video from URL %v", errDownload))
		}
		// Build file path to save the downloaded video temporarily
		oriVideoPath = fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+fileName)
		if errWrite := os.WriteFile(oriVideoPath, videoBytes, 0644); errWrite != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to store downloaded video in server %v", errWrite))
		}
	} else if request.Video != nil {
		// Save uploaded video to server
		oriVideoPath = fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+request.Video.Filename)
		err = fasthttp.SaveMultipartFile(request.Video, oriVideoPath)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to store video in server %v", err))
		}
	} else {
		// This should not happen due to validation, but guard anyway
		return response, pkgError.ValidationError("either Video or VideoURL must be provided")
	}

	// Check if ffmpeg is installed
	_, err = exec.LookPath("ffmpeg")
	if err != nil {
		return response, pkgError.InternalServerError("ffmpeg not installed")
	}

	// Generate thumbnail using ffmpeg
	thumbnailVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+".png")
	cmdThumbnail := exec.Command("ffmpeg", "-i", oriVideoPath, "-ss", "00:00:01.000", "-vframes", "1", thumbnailVideoPath)
	err = cmdThumbnail.Run()
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to create thumbnail %v", err))
	}

	// Resize Thumbnail
	srcImage, err := imaging.Open(thumbnailVideoPath)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("Failed to open generated video thumbnail image '%s': %v. Possible causes: file not found, unsupported format, or permission denied.", thumbnailVideoPath, err))
	}
	resizedImage := imaging.Resize(srcImage, 100, 0, imaging.Lanczos)
	thumbnailResizeVideoPath := fmt.Sprintf("%s/thumbnails-%s", config.PathSendItems, generateUUID+".png")
	if err = imaging.Save(resizedImage, thumbnailResizeVideoPath); err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to save thumbnail %v", err))
	}

	deletedItems = append(deletedItems, thumbnailVideoPath)
	deletedItems = append(deletedItems, thumbnailResizeVideoPath)
	videoThumbnail = thumbnailResizeVideoPath

	// Compress if requested
	if request.Compress {
		compresVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+".mp4")

		// Use proper compression settings to reduce file size
		// -crf 28: Constant Rate Factor (18-28 is good range, higher = smaller file)
		// -preset medium: Balance between encoding speed and compression efficiency
		// -c:v libx264: Use H.264 codec for video
		// -c:a aac: Use AAC codec for audio
		// -movflags +faststart: Optimize for web streaming
		// -vf scale=720:-2: Scale video to max width 720px, maintain aspect ratio
		cmdCompress := exec.Command("ffmpeg", "-i", oriVideoPath,
			"-c:v", "libx264",
			"-crf", "28",
			"-preset", "fast",
			"-vf", "scale=720:-2",
			"-c:a", "aac",
			"-b:a", "128k",
			"-movflags", "+faststart",
			"-y", // Overwrite output file if it exists
			compresVideoPath)

		// Capture both stdout and stderr for better error reporting
		output, err := cmdCompress.CombinedOutput()
		if err != nil {
			logrus.Errorf("ffmpeg compression failed: %v, output: %s", err, string(output))
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to compress video: %v", err))
		}

		videoPath = compresVideoPath
		deletedItems = append(deletedItems, compresVideoPath)
	} else {
		videoPath = oriVideoPath
	}
	deletedItems = append(deletedItems, oriVideoPath)

	//Send to WA server
	dataWaVideo, err := os.ReadFile(videoPath)
	if err != nil {
		return response, err
	}
	uploaded, err := service.uploadMedia(ctx, whatsmeow.MediaVideo, dataWaVideo, dataWaRecipient)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("Failed to upload file: %v", err))
	}
	dataWaThumbnail, err := os.ReadFile(videoThumbnail)
	if err != nil {
		return response, err
	}

	msg := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		URL:                 proto.String(uploaded.URL),
		Mimetype:            proto.String(http.DetectContentType(dataWaVideo)),
		Caption:             proto.String(request.Caption),
		FileLength:          proto.Uint64(uploaded.FileLength),
		FileSHA256:          uploaded.FileSHA256,
		FileEncSHA256:       uploaded.FileEncSHA256,
		MediaKey:            uploaded.MediaKey,
		DirectPath:          proto.String(uploaded.DirectPath),
		ViewOnce:            proto.Bool(request.ViewOnce),
		JPEGThumbnail:       dataWaThumbnail,
		ThumbnailEncSHA256:  dataWaThumbnail,
		ThumbnailSHA256:     dataWaThumbnail,
		ThumbnailDirectPath: proto.String(uploaded.DirectPath),
	}}

	if request.BaseRequest.IsForwarded {
		msg.VideoMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.VideoMessage.ContextInfo == nil {
			msg.VideoMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.VideoMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	caption := "🎥 Video"
	if request.Caption != "" {
		caption = "🎥 " + request.Caption
	}
	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, caption)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Video sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendContact(ctx context.Context, request domainSend.ContactRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendContact(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	msgVCard := fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nN:;%v;;;\nFN:%v\nTEL;type=CELL;waid=%v:+%v\nEND:VCARD",
		request.ContactName, request.ContactName, request.ContactPhone, request.ContactPhone)
	msg := &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String(request.ContactName),
		Vcard:       proto.String(msgVCard),
	}}

	if request.BaseRequest.IsForwarded {
		msg.ContactMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.ContactMessage.ContextInfo == nil {
			msg.ContactMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.ContactMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	content := "👤 " + request.ContactName

	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, content)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Contact sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendLink(ctx context.Context, request domainSend.LinkRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendLink(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	metadata, err := utils.GetMetaDataFromURL(request.Link)
	if err != nil {
		return response, err
	}

	// Log image dimensions if available, otherwise note it's a square image or dimensions not available
	if metadata.Width != nil && metadata.Height != nil {
		logrus.Debugf("Image dimensions: %dx%d", *metadata.Width, *metadata.Height)
	} else {
		logrus.Debugf("Image dimensions: Square image or dimensions not available")
	}

	// Create the message
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:          proto.String(fmt.Sprintf("%s\n%s", request.Caption, request.Link)),
		Title:         proto.String(metadata.Title),
		MatchedText:   proto.String(request.Link),
		Description:   proto.String(metadata.Description),
		JPEGThumbnail: metadata.ImageThumb,
	}}

	if request.BaseRequest.IsForwarded {
		msg.ExtendedTextMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.ExtendedTextMessage.ContextInfo == nil {
			msg.ExtendedTextMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.ExtendedTextMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	// If we have a thumbnail image, upload it to WhatsApp's servers
	if len(metadata.ImageThumb) > 0 && metadata.Height != nil && metadata.Width != nil {
		uploadedThumb, err := service.uploadMedia(ctx, whatsmeow.MediaLinkThumbnail, metadata.ImageThumb, dataWaRecipient)
		if err == nil {
			// Update the message with the uploaded thumbnail information
			msg.ExtendedTextMessage.ThumbnailDirectPath = proto.String(uploadedThumb.DirectPath)
			msg.ExtendedTextMessage.ThumbnailSHA256 = uploadedThumb.FileSHA256
			msg.ExtendedTextMessage.ThumbnailEncSHA256 = uploadedThumb.FileEncSHA256
			msg.ExtendedTextMessage.MediaKey = uploadedThumb.MediaKey
			msg.ExtendedTextMessage.ThumbnailHeight = metadata.Height
			msg.ExtendedTextMessage.ThumbnailWidth = metadata.Width
		} else {
			logrus.Warnf("Failed to upload thumbnail: %v, continue without uploaded thumbnail", err)
		}
	}

	content := "🔗 " + request.Link
	if request.Caption != "" {
		content = "🔗 " + request.Caption
	}
	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, content)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Link sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendLocation(ctx context.Context, request domainSend.LocationRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendLocation(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	// Compose WhatsApp Proto
	msg := &waE2E.Message{
		LocationMessage: &waE2E.LocationMessage{
			DegreesLatitude:  proto.Float64(utils.StrToFloat64(request.Latitude)),
			DegreesLongitude: proto.Float64(utils.StrToFloat64(request.Longitude)),
		},
	}

	if request.BaseRequest.IsForwarded {
		msg.LocationMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.LocationMessage.ContextInfo == nil {
			msg.LocationMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.LocationMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	content := "📍 " + request.Latitude + ", " + request.Longitude

	// Send WhatsApp Message Proto
	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, content)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Send location success %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendAudio(ctx context.Context, request domainSend.AudioRequest) (response domainSend.GenericResponse, err error) {
	// Validate request
	err = validations.ValidateSendAudio(ctx, request)
	if err != nil {
		return response, err
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	var (
		audioBytes    []byte
		audioMimeType string
	)

	// Handle audio from URL or file
	if request.AudioURL != nil && *request.AudioURL != "" {
		audioBytes, _, err = utils.DownloadAudioFromURL(*request.AudioURL)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to download audio from URL %v", err))
		}
		audioMimeType = http.DetectContentType(audioBytes)
	} else if request.Audio != nil {
		audioBytes = helpers.MultipartFormFileHeaderToBytes(request.Audio)
		audioMimeType = http.DetectContentType(audioBytes)
	}

	// upload to WhatsApp servers
	audioUploaded, err := service.uploadMedia(ctx, whatsmeow.MediaAudio, audioBytes, dataWaRecipient)
	if err != nil {
		err = pkgError.WaUploadMediaError(fmt.Sprintf("Failed to upload audio: %v", err))
		return response, err
	}

	msg := &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(audioUploaded.URL),
			DirectPath:    proto.String(audioUploaded.DirectPath),
			Mimetype:      proto.String(audioMimeType),
			FileLength:    proto.Uint64(audioUploaded.FileLength),
			FileSHA256:    audioUploaded.FileSHA256,
			FileEncSHA256: audioUploaded.FileEncSHA256,
			MediaKey:      audioUploaded.MediaKey,
		},
	}

	if request.BaseRequest.IsForwarded {
		msg.AudioMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.AudioMessage.ContextInfo == nil {
			msg.AudioMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.AudioMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	content := "🎵 Audio"

	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, content)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Send audio success %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendPoll(ctx context.Context, request domainSend.PollRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendPoll(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

	content := "📊 " + request.Question

	msg := whatsapp.GetClient().BuildPollCreation(request.Question, request.Options, request.MaxAnswer)

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.PollCreationMessage.ContextInfo == nil {
			msg.PollCreationMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.PollCreationMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, content)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Send poll success %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) SendPresence(ctx context.Context, request domainSend.PresenceRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendPresence(ctx, request)
	if err != nil {
		return response, err
	}

	err = whatsapp.GetClient().SendPresence(ctx, types.Presence(request.Type))
	if err != nil {
		return response, err
	}

	response.MessageID = "presence"
	response.Status = fmt.Sprintf("Send presence success %s", request.Type)
	return response, nil
}

func (service serviceSend) SendChatPresence(ctx context.Context, request domainSend.ChatPresenceRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendChatPresence(ctx, request)
	if err != nil {
		return response, err
	}

	userJid, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	var presenceType types.ChatPresence
	var messageID string
	var statusMessage string

	switch request.Action {
	case "start":
		presenceType = types.ChatPresenceComposing
		messageID = "chat-presence-start"
		statusMessage = fmt.Sprintf("Send chat presence start typing success %s", request.Phone)
	case "stop":
		presenceType = types.ChatPresencePaused
		messageID = "chat-presence-stop"
		statusMessage = fmt.Sprintf("Send chat presence stop typing success %s", request.Phone)
	default:
		return response, fmt.Errorf("invalid action: %s. Must be 'start' or 'stop'", request.Action)
	}

	err = whatsapp.GetClient().SendChatPresence(ctx, userJid, presenceType, types.ChatPresenceMedia(""))
	if err != nil {
		return response, err
	}

	response.MessageID = messageID
	response.Status = statusMessage
	return response, nil
}

func (service serviceSend) getMentionFromText(_ context.Context, messages string) (result []string) {
	mentions := utils.ContainsMention(messages)
	for _, mention := range mentions {
		// Get JID from phone number
		if dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), mention); err == nil {
			result = append(result, dataWaRecipient.String())
		}
	}
	return result
}

func (service serviceSend) SendSticker(ctx context.Context, request domainSend.StickerRequest) (response domainSend.GenericResponse, err error) {
	// Validate request
	err = validations.ValidateSendSticker(ctx, request)
	if err != nil {
		return response, err
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	var (
		stickerPath  string
		deletedItems []string
		stickerBytes []byte
	)

	// Resolve absolute base directory for send items
	absBaseDir, err := filepath.Abs(config.PathSendItems)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to resolve base directory: %v", err))
	}

	defer func() {
		// Delete temporary files
		for _, path := range deletedItems {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logrus.Warnf("Failed to cleanup temporary file %s: %v", path, err)
			}
		}
	}()

	// Handle sticker from URL or file
	if request.StickerURL != nil && *request.StickerURL != "" {
		// Download sticker from URL
		imageData, _, err := utils.DownloadImageFromURL(*request.StickerURL)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to download sticker from URL: %v", err))
		}

		// Create safe temporary file within base dir
		f, err := os.CreateTemp(absBaseDir, "sticker_*")
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to create temp file: %v", err))
		}
		stickerPath = f.Name()
		if _, err := f.Write(imageData); err != nil {
			f.Close()
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to write sticker: %v", err))
		}
		_ = f.Close()
		deletedItems = append(deletedItems, stickerPath)
	} else if request.Sticker != nil {
		// Create safe temporary file within base dir
		f, err := os.CreateTemp(absBaseDir, "sticker_*")
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to create temp file: %v", err))
		}
		stickerPath = f.Name()
		_ = f.Close()

		// Save uploaded file to safe path
		err = fasthttp.SaveMultipartFile(request.Sticker, stickerPath)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to save sticker: %v", err))
		}
		deletedItems = append(deletedItems, stickerPath)
	}

	// Convert image to WebP format for sticker (512x512 max size)
	srcImage, err := imaging.Open(stickerPath)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to open image for sticker conversion: %v", err))
	}

	// Resize image to max 512x512 maintaining aspect ratio
	bounds := srcImage.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width > 512 || height > 512 {
		if width > height {
			srcImage = imaging.Resize(srcImage, 512, 0, imaging.Lanczos)
		} else {
			srcImage = imaging.Resize(srcImage, 0, 512, imaging.Lanczos)
		}
	}

	// Convert to WebP using external command (ffmpeg or cwebp)
	webpPath := filepath.Join(absBaseDir, fmt.Sprintf("sticker_%s.webp", fiberUtils.UUIDv4()))
	deletedItems = append(deletedItems, webpPath)

	// First save as PNG temporarily
	pngPath := filepath.Join(absBaseDir, fmt.Sprintf("temp_%s.png", fiberUtils.UUIDv4()))
	deletedItems = append(deletedItems, pngPath)

	err = imaging.Save(srcImage, pngPath)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to save temporary PNG: %v", err))
	}

	// Try to use ffmpeg first (most common), then cwebp
	var convertCmd *exec.Cmd

	// Add execution timeout for conversion
	convCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	// Check if ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		// Use ffmpeg to convert to WebP with transparency support, overwrite if exists
		convertCmd = exec.CommandContext(convCtx, "ffmpeg", "-y", "-i", pngPath, "-vcodec", "libwebp", "-lossless", "0", "-compression_level", "6", "-q:v", "60", "-preset", "default", "-loop", "0", "-an", "-vsync", "0", webpPath)
	} else if _, err := exec.LookPath("cwebp"); err == nil {
		// Use cwebp as fallback
		convertCmd = exec.CommandContext(convCtx, "cwebp", "-q", "60", "-o", webpPath, pngPath)
	} else {
		// If neither tool is available, return error
		return response, pkgError.InternalServerError("neither ffmpeg nor cwebp is installed for WebP conversion")
	}

	var stderr bytes.Buffer
	convertCmd.Stderr = &stderr

	if err := convertCmd.Run(); err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to convert sticker to WebP: %v, stderr: %s", err, stderr.String()))
	}

	// Read the WebP file
	stickerBytes, err = os.ReadFile(webpPath)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to read WebP sticker: %v", err))
	}

	// Upload sticker to WhatsApp servers
	stickerUploaded, err := service.uploadMedia(ctx, whatsmeow.MediaImage, stickerBytes, dataWaRecipient)
	if err != nil {
		return response, pkgError.WaUploadMediaError(fmt.Sprintf("failed to upload sticker: %v", err))
	}

	// Create sticker message
	msg := &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			URL:           proto.String(stickerUploaded.URL),
			DirectPath:    proto.String(stickerUploaded.DirectPath),
			Mimetype:      proto.String("image/webp"),
			FileLength:    proto.Uint64(stickerUploaded.FileLength),
			FileSHA256:    stickerUploaded.FileSHA256,
			FileEncSHA256: stickerUploaded.FileEncSHA256,
			MediaKey:      stickerUploaded.MediaKey,
			Width:         proto.Uint32(uint32(srcImage.Bounds().Dx())),
			Height:        proto.Uint32(uint32(srcImage.Bounds().Dy())),
			IsAnimated:    proto.Bool(false),
		},
	}

	if request.BaseRequest.IsForwarded {
		msg.StickerMessage.ContextInfo = &waE2E.ContextInfo{
			IsForwarded:     proto.Bool(true),
			ForwardingScore: proto.Uint32(100),
		}
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		if msg.StickerMessage.ContextInfo == nil {
			msg.StickerMessage.ContextInfo = &waE2E.ContextInfo{}
		}
		msg.StickerMessage.ContextInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	}

	content := "🎨 Sticker"

	// Send the sticker message
	ts, err := service.wrapSendMessage(ctx, dataWaRecipient, msg, content)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Sticker sent to %s (server timestamp: %s)", request.Phone, ts.Timestamp.String())
	return response, nil
}

func (service serviceSend) uploadMedia(ctx context.Context, mediaType whatsmeow.MediaType, media []byte, recipient types.JID) (uploaded whatsmeow.UploadResponse, err error) {
	if recipient.Server == types.NewsletterServer {
		uploaded, err = whatsapp.GetClient().UploadNewsletter(ctx, media, mediaType)
	} else {
		uploaded, err = whatsapp.GetClient().Upload(ctx, media, mediaType)
	}
	return uploaded, err
}

func (service serviceSend) getDefaultEphemeralExpiration(jid string) (expiration uint32) {
	expiration = 0
	if jid == "" {
		return expiration
	}

	chat, err := service.chatStorageRepo.GetChat(jid)
	if err != nil {
		return expiration
	}

	if chat != nil && chat.EphemeralExpiration != 0 {
		expiration = chat.EphemeralExpiration
	}

	return expiration
}
