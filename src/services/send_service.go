package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	"github.com/h2non/bimg"
	"github.com/valyala/fasthttp"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
	"net/http"
	"os"
	"os/exec"
)

type serviceSend struct {
	WaCli *whatsmeow.Client
}

func NewSendService(waCli *whatsmeow.Client) domainSend.ISendService {
	return &serviceSend{
		WaCli: waCli,
	}
}

func (service serviceSend) SendText(ctx context.Context, request domainSend.MessageRequest) (response domainSend.MessageResponse, err error) {
	utils.MustLogin(service.WaCli)

	recipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}

	msgId := whatsmeow.GenerateMessageID()
	msg := &waProto.Message{Conversation: proto.String(request.Message)}
	ts, err := service.WaCli.SendMessage(ctx, recipient, msgId, msg)
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Message sent to %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}

func (service serviceSend) SendImage(ctx context.Context, request domainSend.ImageRequest) (response domainSend.ImageResponse, err error) {
	utils.MustLogin(service.WaCli)

	var (
		imagePath      string
		imageThumbnail string
		deletedItems   []string
	)

	// Save image to server
	oriImagePath := fmt.Sprintf("%s/%s", config.PathSendItems, request.Image.Filename)
	err = fasthttp.SaveMultipartFile(request.Image, oriImagePath)
	if err != nil {
		return response, err
	}
	deletedItems = append(deletedItems, oriImagePath)

	// Generate thumbnail with smalled image
	openThumbnailBuffer, err := bimg.Read(oriImagePath)
	imageThumbnail = fmt.Sprintf("%s/thumbnails-%s", config.PathSendItems, request.Image.Filename)
	thumbnailImage, err := bimg.NewImage(openThumbnailBuffer).Process(bimg.Options{Quality: 90, Width: 100, Embed: true})
	if err != nil {
		return response, err
	}
	err = bimg.Write(imageThumbnail, thumbnailImage)
	if err != nil {
		return response, err
	}
	deletedItems = append(deletedItems, imageThumbnail)

	if request.Compress {
		// Resize image
		openImageBuffer, err := bimg.Read(oriImagePath)
		newImage, err := bimg.NewImage(openImageBuffer).Process(bimg.Options{Quality: 90, Width: 600, Embed: true})
		if err != nil {
			return response, err
		}

		newImagePath := fmt.Sprintf("%s/new-%s", config.PathSendItems, request.Image.Filename)
		err = bimg.Write(newImagePath, newImage)
		if err != nil {
			return response, err
		}
		deletedItems = append(deletedItems, newImagePath)
		imagePath = newImagePath
	} else {
		imagePath = oriImagePath
	}

	// Send to WA server
	dataWaCaption := request.Caption
	dataWaRecipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}
	dataWaImage, err := os.ReadFile(imagePath)
	if err != nil {
		return response, err
	}
	uploadedImage, err := service.WaCli.Upload(context.Background(), dataWaImage, whatsmeow.MediaImage)
	if err != nil {
		fmt.Printf("Failed to upload file: %v", err)
		return response, err
	}
	dataWaThumbnail, err := os.ReadFile(imageThumbnail)

	msgId := whatsmeow.GenerateMessageID()
	msg := &waProto.Message{ImageMessage: &waProto.ImageMessage{
		JpegThumbnail: dataWaThumbnail,
		Caption:       proto.String(dataWaCaption),
		Url:           proto.String(uploadedImage.URL),
		DirectPath:    proto.String(uploadedImage.DirectPath),
		MediaKey:      uploadedImage.MediaKey,
		Mimetype:      proto.String(http.DetectContentType(dataWaImage)),
		FileEncSha256: uploadedImage.FileEncSHA256,
		FileSha256:    uploadedImage.FileSHA256,
		FileLength:    proto.Uint64(uint64(len(dataWaImage))),
		ViewOnce:      proto.Bool(request.ViewOnce),
	}}
	ts, err := service.WaCli.SendMessage(ctx, dataWaRecipient, msgId, msg)
	go func() {
		errDelete := utils.RemoveFile(0, deletedItems...)
		if errDelete != nil {
			fmt.Println("error when deleting picture: ", errDelete)
		}
	}()
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Message sent to %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}

func (service serviceSend) SendFile(ctx context.Context, request domainSend.FileRequest) (response domainSend.FileResponse, err error) {
	utils.MustLogin(service.WaCli)

	oriFilePath := fmt.Sprintf("%s/%s", config.PathSendItems, request.File.Filename)
	err = fasthttp.SaveMultipartFile(request.File, oriFilePath)
	if err != nil {
		return response, err
	}

	// Send to WA server
	dataWaRecipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}
	dataWaFile, err := os.ReadFile(oriFilePath)
	if err != nil {
		return response, err
	}
	uploadedFile, err := service.WaCli.Upload(context.Background(), dataWaFile, whatsmeow.MediaDocument)
	if err != nil {
		fmt.Printf("Failed to upload file: %v", err)
		return response, err
	}

	msgId := whatsmeow.GenerateMessageID()
	msg := &waProto.Message{DocumentMessage: &waProto.DocumentMessage{
		Url:           proto.String(uploadedFile.URL),
		Mimetype:      proto.String(http.DetectContentType(dataWaFile)),
		Title:         proto.String(request.File.Filename),
		FileSha256:    uploadedFile.FileSHA256,
		FileLength:    proto.Uint64(uploadedFile.FileLength),
		MediaKey:      uploadedFile.MediaKey,
		FileName:      proto.String(request.File.Filename),
		FileEncSha256: uploadedFile.FileEncSHA256,
		DirectPath:    proto.String(uploadedFile.DirectPath),
	}}
	ts, err := service.WaCli.SendMessage(ctx, dataWaRecipient, msgId, msg)
	go func() {
		errDelete := utils.RemoveFile(0, oriFilePath)
		if errDelete != nil {
			fmt.Println(errDelete)
		}
	}()
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Document sent to %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}

func (service serviceSend) SendVideo(ctx context.Context, request domainSend.VideoRequest) (response domainSend.VideoResponse, err error) {
	utils.MustLogin(service.WaCli)

	var (
		videoPath      string
		videoThumbnail string
		deletedItems   []string
	)

	generateUUID := fiberUtils.UUIDv4()
	// Save video to server
	oriVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+request.Video.Filename)
	err = fasthttp.SaveMultipartFile(request.Video, oriVideoPath)
	if err != nil {
		return response, err
	}

	// Get thumbnail video with ffmpeg
	thumbnailVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+".png")
	cmdThumbnail := exec.Command("ffmpeg", "-i", oriVideoPath, "-ss", "00:00:01.000", "-vframes", "1", thumbnailVideoPath)
	err = cmdThumbnail.Run()
	utils.PanicIfNeeded(err, "error when getting thumbnail")

	// Resize Thumbnail
	openImageBuffer, err := bimg.Read(thumbnailVideoPath)
	resize, err := bimg.NewImage(openImageBuffer).Process(bimg.Options{Quality: 90, Width: 600, Embed: true})
	if err != nil {
		return response, err
	}
	thumbnailResizeVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+"_resize.png")
	err = bimg.Write(thumbnailResizeVideoPath, resize)
	if err != nil {
		return response, err
	}

	deletedItems = append(deletedItems, thumbnailVideoPath)
	deletedItems = append(deletedItems, thumbnailResizeVideoPath)
	videoThumbnail = thumbnailResizeVideoPath

	if request.Compress {
		compresVideoPath := fmt.Sprintf("%s/%s", config.PathSendItems, generateUUID+".mp4")
		// Compress video with ffmpeg
		cmdCompress := exec.Command("ffmpeg", "-i", oriVideoPath, "-strict", "-2", compresVideoPath)
		err = cmdCompress.Run()
		utils.PanicIfNeeded(err, "error when compress video")

		videoPath = compresVideoPath
		deletedItems = append(deletedItems, compresVideoPath)
	} else {
		videoPath = oriVideoPath
		deletedItems = append(deletedItems, oriVideoPath)
	}

	//Send to WA server
	dataWaRecipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}
	dataWaVideo, err := os.ReadFile(videoPath)
	if err != nil {
		return response, err
	}
	uploaded, err := service.WaCli.Upload(context.Background(), dataWaVideo, whatsmeow.MediaVideo)
	if err != nil {
		fmt.Printf("Failed to upload file: %v", err)
		return response, err
	}
	dataWaThumbnail, err := os.ReadFile(videoThumbnail)
	if err != nil {
		return response, err
	}

	msgId := whatsmeow.GenerateMessageID()
	msg := &waProto.Message{VideoMessage: &waProto.VideoMessage{
		Url:           proto.String(uploaded.URL),
		Mimetype:      proto.String(http.DetectContentType(dataWaVideo)),
		Caption:       proto.String(request.Caption),
		FileLength:    proto.Uint64(uploaded.FileLength),
		FileSha256:    uploaded.FileSHA256,
		FileEncSha256: uploaded.FileEncSHA256,
		MediaKey:      uploaded.MediaKey,
		DirectPath:    proto.String(uploaded.DirectPath),
		ViewOnce:      proto.Bool(request.ViewOnce),
		JpegThumbnail: dataWaThumbnail,
	}}
	ts, err := service.WaCli.SendMessage(ctx, dataWaRecipient, msgId, msg)
	go func() {
		errDelete := utils.RemoveFile(0, deletedItems...)
		if errDelete != nil {
			fmt.Println(errDelete)
		}
	}()
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Video sent to %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}

func (service serviceSend) SendContact(ctx context.Context, request domainSend.ContactRequest) (response domainSend.ContactResponse, err error) {
	utils.MustLogin(service.WaCli)

	recipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}
	msgVCard := fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nN:;%v;;;\nFN:%v\nTEL;type=CELL;waid=%v:+%v\nEND:VCARD",
		request.ContactName, request.ContactName, request.ContactPhone, request.ContactPhone)
	msgId := whatsmeow.GenerateMessageID()
	msg := &waProto.Message{ContactMessage: &waProto.ContactMessage{
		DisplayName: proto.String(request.ContactName),
		Vcard:       proto.String(msgVCard),
	}}
	ts, err := service.WaCli.SendMessage(ctx, recipient, "", msg)
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Contact sent to %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}

func (service serviceSend) SendLink(ctx context.Context, request domainSend.LinkRequest) (response domainSend.LinkResponse, err error) {
	utils.MustLogin(service.WaCli)

	recipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}

	msgId := whatsmeow.GenerateMessageID()
	msg := &waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{
		Text:         proto.String(request.Caption),
		MatchedText:  proto.String(request.Caption),
		CanonicalUrl: proto.String(request.Link),
		ContextInfo: &waProto.ContextInfo{
			ActionLink: &waProto.ActionLink{
				Url:         proto.String(request.Link),
				ButtonTitle: proto.String(request.Caption),
			},
		},
	}}
	ts, err := service.WaCli.SendMessage(ctx, recipient, msgId, msg)
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Link sent to %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}

func (service serviceSend) Revoke(_ context.Context, request domainSend.RevokeRequest) (response domainSend.RevokeResponse, err error) {
	utils.MustLogin(service.WaCli)

	recipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}

	msgId := whatsmeow.GenerateMessageID()
	ts, err := service.WaCli.SendMessage(context.Background(), recipient, msgId, service.WaCli.BuildRevoke(recipient, types.EmptyJID, request.MessageID))
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Revoke success %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}

func (service serviceSend) UpdateMessage(ctx context.Context, request domainSend.UpdateMessageRequest) (response domainSend.UpdateMessageResponse, err error) {
	utils.MustLogin(service.WaCli)

	recipient, ok := utils.ParseJID(request.Phone)
	if !ok {
		return response, errors.New("invalid JID " + request.Phone)
	}

	msgId := whatsmeow.GenerateMessageID()
	msg := &waProto.Message{Conversation: proto.String(request.Message)}
	ts, err := service.WaCli.SendMessage(context.Background(), recipient, msgId, service.WaCli.BuildEdit(recipient, request.MessageID, msg))
	if err != nil {
		return response, err
	}

	response.MessageID = msgId
	response.Status = fmt.Sprintf("Update message success %s (server timestamp: %s)", request.Phone, ts)
	return response, nil
}
