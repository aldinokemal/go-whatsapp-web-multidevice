package usecase

import (
	"bytes"
	"context"
	"fmt"
	"image/gif"
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
	chaiWebp "github.com/chai2010/webp"
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

func (service *serviceSend) wrapAndStoreMessage(ctx context.Context, recipient types.JID, msg *waE2E.Message, content string, mediaInfo *domainChatStorage.MediaInfo) (whatsmeow.SendResponse, error) {
	ts, err := whatsapp.GetClient().SendMessage(ctx, recipient, msg)
	if err != nil {
		return whatsmeow.SendResponse{}, err
	}

	senderJID := ""
	if whatsapp.GetClient().Store.ID != nil {
		senderJID = whatsapp.GetClient().Store.ID.String()
	}

	go func() {
		storeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := service.storeMessage(storeCtx, ts.ID, senderJID, recipient.String(), content, ts.Timestamp, mediaInfo); err != nil {
			logrus.Errorf("Failed to store sent message with media info: %v", err)
		}
	}()

	return ts, nil
}

func (service *serviceSend) storeMessage(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time, mediaInfo *domainChatStorage.MediaInfo) error {
	if service.chatStorageRepo == nil {
		return fmt.Errorf("chat storage repository is not initialized")
	}

	jid, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), recipientJID)
	if err != nil {
		return fmt.Errorf("invalid recipient JID: %w", err)
	}
	chatJID := jid.String()

	message := &domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   chatJID,
		Sender:    senderJID,
		Content:   content,
		Timestamp: timestamp,
		IsFromMe:  true,
	}

	if mediaInfo != nil {
		message.MediaType = mediaInfo.MediaType
		message.Filename = mediaInfo.Filename
		message.URL = mediaInfo.URL
		message.MediaKey = mediaInfo.MediaKey
		message.FileSHA256 = mediaInfo.FileSHA256
		message.FileEncSHA256 = mediaInfo.FileEncSHA256
		message.FileLength = mediaInfo.FileLength
	}

	return service.chatStorageRepo.StoreMessage(message)
}

func (service *serviceSend) SendText(ctx context.Context, request domainSend.MessageRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendMessage(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
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
			participantJID := message.Sender

			ctxInfo := &waE2E.ContextInfo{
				StanzaID:    request.ReplyMessageID,
				Participant: proto.String(participantJID),
				QuotedMessage: &waE2E.Message{
					Conversation: proto.String(message.Content),
				},
			}

			if request.BaseRequest.IsForwarded {
				ctxInfo.IsForwarded = proto.Bool(true)
				ctxInfo.ForwardingScore = proto.Uint32(100)
			}

			if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
				ctxInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
			} else {
				ctxInfo.Expiration = proto.Uint32(service.getDefaultEphemeralExpiration(participantJID))
			}

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

	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, request.Message, nil)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Message sent to %s (server timestamp: %s)", request.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) SendImage(ctx context.Context, request domainSend.ImageRequest) (response domainSend.GenericResponse, err error) {
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
		imageData, fileName, err := utils.DownloadImageFromURL(*request.ImageURL)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to download image from URL %v", err))
		}

		oriImagePath = fmt.Sprintf("%s/%s", config.PathSendItems, fileName)
		imageName = fileName
		err = os.WriteFile(oriImagePath, imageData, 0644)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to save downloaded image %v", err))
		}
	} else if request.Image != nil {
		oriImagePath = fmt.Sprintf("%s/%s", config.PathSendItems, request.Image.Filename)
		err = fasthttp.SaveMultipartFile(request.Image, oriImagePath)
		if err != nil {
			return response, err
		}
		imageName = request.Image.Filename
	}
	deletedItems = append(deletedItems, oriImagePath)

	srcImage, err := imaging.Open(oriImagePath)
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("Failed to open image file '%s' for thumbnail generation: %v. Possible causes: file not found, unsupported format, or permission denied.", oriImagePath, err))
	}

	resizedImage := imaging.Resize(srcImage, 100, 0, imaging.Lanczos)
	imageThumbnail = fmt.Sprintf("%s/thumbnails-%s", config.PathSendItems, imageName)
	if err = imaging.Save(resizedImage, imageThumbnail); err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to save thumbnail %v", err))
	}
	deletedItems = append(deletedItems, imageThumbnail)

	if request.Compress {
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
	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, caption, nil)
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

func (service *serviceSend) SendFile(ctx context.Context, request domainSend.FileRequest) (response domainSend.GenericResponse, err error) {
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
	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, caption, nil)
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

func (service *serviceSend) SendVideo(ctx context.Context, request domainSend.VideoRequest) (response domainSend.GenericResponse, err error) {
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

	defer func() {
		if len(deletedItems) > 0 {
			go utils.RemoveFile(1, deletedItems...)
		}
	}()

	generateUUID := fiberUtils.UUIDv4()

	var oriVideoPath string

	if request.VideoURL != nil && *request.VideoURL != "" {
		videoBytes, fileName, errDownload := utils.DownloadVideoFromURL(*request.VideoURL)
		if errDownload != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to download video from URL %v", errDownload))
		}
		oriVideoPath = fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+fileName)
		if errWrite := os.WriteFile(oriVideoPath, videoBytes, 0644); errWrite != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to store downloaded video in server %v", errWrite))
		}
	} else if request.Video != nil {
		oriVideoPath = fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+request.Video.Filename)
		err = fasthttp.SaveMultipartFile(request.Video, oriVideoPath)
		if err != nil {
			return response, pkgError.InternalServerError(fmt.Sprintf("failed to store video in server %v", err))
		}
	} else {
		return response, pkgError.ValidationError("either Video or VideoURL must be provided")
	}

	_, err = exec.LookPath("ffmpeg")
	if err != nil {
		return response, pkgError.InternalServerError("ffmpeg not installed")
	}

	thumbnailVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+".png")
	cmdThumbnail := exec.Command("ffmpeg", "-i", oriVideoPath, "-ss", "00:00:01.000", "-vframes", "1", thumbnailVideoPath)
	err = cmdThumbnail.Run()
	if err != nil {
		return response, pkgError.InternalServerError(fmt.Sprintf("failed to create thumbnail %v", err))
	}

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

	if request.Compress {
		compresVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+".mp4")

		cmdCompress := exec.Command("ffmpeg", "-i", oriVideoPath,
			"-c:v", "libx264",
			"-crf", "28",
			"-preset", "fast",
			"-vf", "scale=720:-2",
			"-c:a", "aac",
			"-b:a", "128k",
			"-movflags", "+faststart",
			"-y",
			compresVideoPath)

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
	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, caption, nil)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Video sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) SendContact(ctx context.Context, request domainSend.ContactRequest) (response domainSend.GenericResponse, err error) {
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

	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, content, nil)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Contact sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) SendLink(ctx context.Context, request domainSend.LinkRequest) (response domainSend.GenericResponse, err error) {
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

	if metadata.Width != nil && metadata.Height != nil {
		logrus.Debugf("Image dimensions: %dx%d", *metadata.Width, *metadata.Height)
	} else {
		logrus.Debugf("Image dimensions: Square image or dimensions not available")
	}

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

	if len(metadata.ImageThumb) > 0 && metadata.Height != nil && metadata.Width != nil {
		uploadedThumb, err := service.uploadMedia(ctx, whatsmeow.MediaLinkThumbnail, metadata.ImageThumb, dataWaRecipient)
		if err == nil {
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
	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, content, nil)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Link sent to %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) SendLocation(ctx context.Context, request domainSend.LocationRequest) (response domainSend.GenericResponse, err error) {
	err = validations.ValidateSendLocation(ctx, request)
	if err != nil {
		return response, err
	}
	dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.BaseRequest.Phone)
	if err != nil {
		return response, err
	}

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

	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, content, nil)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Send location success %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) SendAudio(ctx context.Context, request domainSend.AudioRequest) (response domainSend.GenericResponse, err error) {
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

	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, content, nil)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Send audio success %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) SendPoll(ctx context.Context, request domainSend.PollRequest) (response domainSend.GenericResponse, err error) {
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

	ts, err := service.wrapAndStoreMessage(ctx, dataWaRecipient, msg, content, nil)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Send poll success %s (server timestamp: %s)", request.BaseRequest.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) SendPresence(ctx context.Context, request domainSend.PresenceRequest) (response domainSend.GenericResponse, err error) {
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

func (service *serviceSend) SendChatPresence(ctx context.Context, request domainSend.ChatPresenceRequest) (response domainSend.GenericResponse, err error) {
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

func (service *serviceSend) getMentionFromText(_ context.Context, messages string) (result []string) {
	mentions := utils.ContainsMention(messages)
	for _, mention := range mentions {
		if dataWaRecipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), mention); err == nil {
			result = append(result, dataWaRecipient.String())
		}
	}
	return result
}

func (service *serviceSend) convertGIFToAnimatedWebP(gifBytes []byte, absBaseDir string) ([]byte, error) {
	gifTempPath := filepath.Join(absBaseDir, fmt.Sprintf("temp_gif_%s.gif", fiberUtils.UUIDv4()))
	webpTempPath := filepath.Join(absBaseDir, fmt.Sprintf("temp_webp_%s.webp", fiberUtils.UUIDv4()))
	defer func() {
		os.Remove(gifTempPath)
		os.Remove(webpTempPath)
	}()

	err := os.WriteFile(gifTempPath, gifBytes, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write temporary GIF file: %w", err)
	}

	var cmd *exec.Cmd
	var converted bool

	if _, err := exec.LookPath("gif2webp"); err == nil {
		cmd = exec.Command("gif2webp", "-q", "85", "-m", "4", gifTempPath, "-o", webpTempPath)
		if err := cmd.Run(); err == nil {
			converted = true
		} else {
			logrus.Warnf("gif2webp failed: %v", err)
		}
	}

	if !converted && exec.Command("ffmpeg", "-version").Run() == nil {
		cmd = exec.Command("ffmpeg", "-i", gifTempPath, "-vf", "scale=512:512:force_original_aspect_ratio=decrease", "-q:v", "5", "-y", webpTempPath)
		if err := cmd.Run(); err == nil {
			converted = true
		} else {
			logrus.Warnf("ffmpeg WebP conversion failed: %v", err)
		}
	}

	if !converted {
		if _, err := exec.LookPath("convert"); err == nil {
			cmd = exec.Command("convert", gifTempPath, "-quality", "85", webpTempPath)
			if err := cmd.Run(); err == nil {
				converted = true
			} else {
				logrus.Warnf("ImageMagick conversion failed: %v", err)
			}
		}
	}

	if !converted {
		return nil, fmt.Errorf("no suitable tool found for animated WebP conversion")
	}

	return os.ReadFile(webpTempPath)
}

func (service *serviceSend) convertGIFToWebPStatic(gifBytes []byte) ([]byte, error) {
	reader := bytes.NewReader(gifBytes)
	gifData, err := gif.DecodeAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to decode GIF: %w", err)
	}

	if len(gifData.Image) == 0 {
		return nil, fmt.Errorf("GIF contains no frames")
	}

	firstFrame := gifData.Image[0]
	outputBuf := new(bytes.Buffer)
	opts := &chaiWebp.Options{Lossless: false, Quality: 85}

	err = chaiWebp.Encode(outputBuf, firstFrame, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to encode frame to WebP: %w", err)
	}

	return outputBuf.Bytes(), nil
}

type processedMedia struct {
	Path        string
	Thumbnail   []byte
	Data        []byte
	Mimetype    string
	IsAnimated  bool
	Width       uint32
	Height      uint32
	FileLength  uint64
	cleanupFunc func()
}

func (service *serviceSend) SendSticker(ctx context.Context, request domainSend.StickerRequest) (response domainSend.GenericResponse, err error) {
	if err := validations.ValidateSendSticker(ctx, request); err != nil {
		return response, err
	}

	recipient, err := utils.ValidateJidWithLogin(whatsapp.GetClient(), request.Phone)
	if err != nil {
		return response, err
	}

	stickerPath, cleanup, err := service.getStickerPath(request)
	if err != nil {
		return response, err
	}
	defer cleanup()

	processed, err := service.processSticker(ctx, stickerPath)
	if err != nil {
		return response, err
	}
	defer processed.cleanupFunc()

	uploaded, err := service.uploadMedia(ctx, whatsmeow.MediaImage, processed.Data, recipient)
	if err != nil {
		return response, pkgError.WaUploadMediaError(fmt.Sprintf("failed to upload sticker: %v", err))
	}

	msg := service.buildStickerMessage(request, uploaded, processed)

	ts, err := service.wrapAndStoreMessage(ctx, recipient, msg, "🎨 Sticker", &domainChatStorage.MediaInfo{
		MediaType:     "sticker",
		URL:           uploaded.URL,
		MediaKey:      uploaded.MediaKey,
		FileSHA256:    uploaded.FileSHA256,
		FileEncSHA256: uploaded.FileEncSHA256,
		FileLength:    uploaded.FileLength,
	})
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Sticker sent to %s (server timestamp: %s)", request.Phone, ts.Timestamp.String())
	return response, nil
}

func (service *serviceSend) getStickerPath(request domainSend.StickerRequest) (string, func(), error) {
	absBaseDir, err := filepath.Abs(config.PathSendItems)
	if err != nil {
		return "", nil, pkgError.InternalServerError(fmt.Sprintf("failed to resolve base directory: %v", err))
	}

	var stickerPath string
	var deletedItems []string
	cleanup := func() {
		for _, path := range deletedItems {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logrus.Warnf("Failed to cleanup temporary file %s: %v", path, err)
			}
		}
	}

	if request.StickerURL != nil && *request.StickerURL != "" {
		imageData, _, err := utils.DownloadImageFromURL(*request.StickerURL)
		if err != nil {
			return "", nil, pkgError.InternalServerError(fmt.Sprintf("failed to download sticker from URL: %v", err))
		}

		f, err := os.CreateTemp(absBaseDir, "sticker_*")
		if err != nil {
			return "", nil, pkgError.InternalServerError(fmt.Sprintf("failed to create temp file: %v", err))
		}
		stickerPath = f.Name()
		if _, err := f.Write(imageData); err != nil {
			f.Close()
			cleanup()
			return "", nil, pkgError.InternalServerError(fmt.Sprintf("failed to write sticker: %v", err))
		}
		_ = f.Close()
		deletedItems = append(deletedItems, stickerPath)
	} else if request.Sticker != nil {
		f, err := os.CreateTemp(absBaseDir, "sticker_*")
		if err != nil {
			return "", nil, pkgError.InternalServerError(fmt.Sprintf("failed to create temp file: %v", err))
		}
		stickerPath = f.Name()
		_ = f.Close()

		err = fasthttp.SaveMultipartFile(request.Sticker, stickerPath)
		if err != nil {
			cleanup()
			return "", nil, pkgError.InternalServerError(fmt.Sprintf("failed to save sticker: %v", err))
		}
		deletedItems = append(deletedItems, stickerPath)
	}

	return stickerPath, cleanup, nil
}

func (service *serviceSend) processSticker(ctx context.Context, stickerPath string) (*processedMedia, error) {
	absBaseDir, err := filepath.Abs(config.PathSendItems)
	if err != nil {
		return nil, pkgError.InternalServerError(fmt.Sprintf("failed to resolve base directory: %v", err))
	}

	var deletedItems []string
	cleanup := func() {
		for _, path := range deletedItems {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				logrus.Warnf("Failed to cleanup temporary file %s: %v", path, err)
			}
		}
	}

	inputFileBytes, err := os.ReadFile(stickerPath)
	if err != nil {
		cleanup()
		return nil, pkgError.InternalServerError(fmt.Sprintf("failed to read input file: %v", err))
	}

	var stickerBytes []byte
	var width, height uint32
	var isAnimated bool

	inputMimeType := http.DetectContentType(inputFileBytes)
	if inputMimeType == "image/webp" {
		stickerBytes = inputFileBytes
		webpReader := bytes.NewReader(stickerBytes)
		srcImage, err := imaging.Decode(webpReader)
		if err == nil {
			bounds := srcImage.Bounds()
			width = uint32(bounds.Dx())
			height = uint32(bounds.Dy())
		}
	} else if inputMimeType == "image/gif" {
		reader := bytes.NewReader(inputFileBytes)
		gifData, err := gif.DecodeAll(reader)
		if err != nil {
			cleanup()
			return nil, pkgError.InternalServerError(fmt.Sprintf("failed to decode GIF: %v", err))
		}

		isAnimated = len(gifData.Image) > 1
		if isAnimated {
			stickerBytes, err = service.convertGIFToAnimatedWebP(inputFileBytes, absBaseDir)
			if err != nil {
				logrus.Warnf("Failed to create animated WebP, falling back to static: %v", err)
				isAnimated = false
				stickerBytes, err = service.convertGIFToWebPStatic(inputFileBytes)
				if err != nil {
					cleanup()
					return nil, pkgError.InternalServerError(fmt.Sprintf("failed to convert GIF to WebP: %v", err))
				}
			}
		} else {
			stickerBytes, err = service.convertGIFToWebPStatic(inputFileBytes)
			if err != nil {
				cleanup()
				return nil, pkgError.InternalServerError(fmt.Sprintf("failed to convert GIF to WebP: %v", err))
			}
		}

		if len(gifData.Image) > 0 {
			bounds := gifData.Image[0].Bounds()
			width = uint32(bounds.Dx())
			height = uint32(bounds.Dy())
		}
	} else {
		srcImage, err := imaging.Open(stickerPath)
		if err != nil {
			cleanup()
			return nil, pkgError.InternalServerError(fmt.Sprintf("failed to open image for sticker conversion: %v", err))
		}

		bounds := srcImage.Bounds()
		imgWidth := bounds.Dx()
		imgHeight := bounds.Dy()

		if imgWidth > 512 || imgHeight > 512 {
			if imgWidth > imgHeight {
				srcImage = imaging.Resize(srcImage, 512, 0, imaging.Lanczos)
			} else {
				srcImage = imaging.Resize(srcImage, 0, 512, imaging.Lanczos)
			}
		}

		webpPath := filepath.Join(absBaseDir, fmt.Sprintf("sticker_%s.webp", fiberUtils.UUIDv4()))
		deletedItems = append(deletedItems, webpPath)

		pngPath := filepath.Join(absBaseDir, fmt.Sprintf("temp_%s.png", fiberUtils.UUIDv4()))
		deletedItems = append(deletedItems, pngPath)

		err = imaging.Save(srcImage, pngPath)
		if err != nil {
			cleanup()
			return nil, pkgError.InternalServerError(fmt.Sprintf("failed to save temporary PNG: %v", err))
		}

		convCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()

		var convertCmd *exec.Cmd
		if _, err := exec.LookPath("ffmpeg"); err == nil {
			convertCmd = exec.CommandContext(convCtx, "ffmpeg", "-y", "-i", pngPath, "-vcodec", "libwebp", "-lossless", "0", "-compression_level", "6", "-q:v", "60", "-preset", "default", "-loop", "0", "-an", "-vsync", "0", webpPath)
		} else if _, err := exec.LookPath("cwebp"); err == nil {
			convertCmd = exec.CommandContext(convCtx, "cwebp", "-q", "60", "-o", webpPath, pngPath)
		} else {
			cleanup()
			return nil, pkgError.InternalServerError("neither ffmpeg nor cwebp is installed for WebP conversion")
		}

		var stderr bytes.Buffer
		convertCmd.Stderr = &stderr

		if err := convertCmd.Run(); err != nil {
			cleanup()
			return nil, pkgError.InternalServerError(fmt.Sprintf("failed to convert sticker to WebP: %v, stderr: %s", err, stderr.String()))
		}

		stickerBytes, err = os.ReadFile(webpPath)
		if err != nil {
			cleanup()
			return nil, pkgError.InternalServerError(fmt.Sprintf("failed to read WebP sticker: %v", err))
		}
	}

	if len(stickerBytes) == 0 {
		cleanup()
		return nil, pkgError.InternalServerError("sticker conversion resulted in empty WebP file")
	}

	if width == 0 || height == 0 {
		webpReader := bytes.NewReader(stickerBytes)
		if srcImage, err := imaging.Decode(webpReader); err == nil {
			bounds := srcImage.Bounds()
			width = uint32(bounds.Dx())
			height = uint32(bounds.Dy())
		} else {
			width = 512
			height = 512
		}
	}

	if width > 512 || height > 512 {
		ratio := float64(width) / float64(height)
		if width > height {
			width = 512
			height = uint32(512.0 / ratio)
		} else {
			height = 512
			width = uint32(512.0 * ratio)
		}
	}

	return &processedMedia{
		Data:        stickerBytes,
		Mimetype:    "image/webp",
		IsAnimated:  isAnimated,
		Width:       width,
		Height:      height,
		cleanupFunc: cleanup,
	}, nil
}

func (service *serviceSend) buildStickerMessage(request domainSend.StickerRequest, uploaded whatsmeow.UploadResponse, media *processedMedia) *waE2E.Message {
	stickerMsg := &waE2E.StickerMessage{
		URL:           proto.String(uploaded.URL),
		DirectPath:    proto.String(uploaded.DirectPath),
		Mimetype:      proto.String(media.Mimetype),
		FileLength:    proto.Uint64(uploaded.FileLength),
		FileSHA256:    uploaded.FileSHA256,
		FileEncSHA256: uploaded.FileEncSHA256,
		MediaKey:      uploaded.MediaKey,
		IsAnimated:    proto.Bool(media.IsAnimated),
		Width:         proto.Uint32(media.Width),
		Height:        proto.Uint32(media.Height),
	}

	msg := &waE2E.Message{StickerMessage: stickerMsg}
	ephemeralExpiration := service.getDefaultEphemeralExpiration(request.Phone)
	ctxInfo := &waE2E.ContextInfo{}

	if request.BaseRequest.IsForwarded {
		ctxInfo.IsForwarded = proto.Bool(true)
		ctxInfo.ForwardingScore = proto.Uint32(100)
	}

	if request.BaseRequest.Duration != nil && *request.BaseRequest.Duration > 0 {
		ctxInfo.Expiration = proto.Uint32(uint32(*request.BaseRequest.Duration))
	} else {
		ctxInfo.Expiration = proto.Uint32(ephemeralExpiration)
	}
	msg.StickerMessage.ContextInfo = ctxInfo
	return msg
}

func (service *serviceSend) uploadMedia(ctx context.Context, mediaType whatsmeow.MediaType, media []byte, recipient types.JID) (uploaded whatsmeow.UploadResponse, err error) {
	if recipient.Server == types.NewsletterServer {
		uploaded, err = whatsapp.GetClient().UploadNewsletter(ctx, media, mediaType)
	} else {
		uploaded, err = whatsapp.GetClient().Upload(ctx, media, mediaType)
	}
	return uploaded, err
}

func (service *serviceSend) getDefaultEphemeralExpiration(jid string) (expiration uint32) {
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
