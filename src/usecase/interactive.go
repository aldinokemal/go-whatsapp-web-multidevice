package usecase

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"github.com/disintegration/imaging"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strconv"
	"time"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// nativeFlow maps validated action parameters to WhatsApp native-flow buttons.
func nativeFlow(body, footer string, buttons []domainSend.NativeFlowButton) *waE2E.InteractiveMessage {
	flow := &waE2E.InteractiveMessage_NativeFlowMessage{MessageVersion: proto.Int32(3)}
	for _, b := range buttons {
		flow.Buttons = append(flow.Buttons, &waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{Name: proto.String(b.Name), ButtonParamsJSON: proto.String(string(b.Params))})
	}
	return &waE2E.InteractiveMessage{Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)}, Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String(footer)}, InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: flow}}
}

// buildInteractive preserves the unwrapped location envelope and wraps other interactive messages.
func buildInteractive(req domainSend.InteractiveRequest) *waE2E.Message {
	interactive := nativeFlow(req.Body, req.Footer, req.Buttons)
	if len(req.Buttons) == 1 && req.Buttons[0].Name == "send_location" {
		interactive.Header = &waE2E.InteractiveMessage_Header{Title: proto.String(""), Subtitle: proto.String(""), HasMediaAttachment: proto.Bool(false)}
		interactive.GetNativeFlowMessage().MessageVersion = nil
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			panic(err)
		}
		return &waE2E.Message{InteractiveMessage: interactive, MessageContextInfo: &waE2E.MessageContextInfo{DeviceListMetadata: &waE2E.DeviceListMetadata{}, DeviceListMetadataVersion: proto.Int32(2), MessageSecret: secret}}
	}
	if len(req.Cards) > 0 {
		carousel := &waE2E.InteractiveMessage_CarouselMessage{MessageVersion: proto.Int32(1)}
		for _, c := range req.Cards {
			card := nativeFlow(c.Body, c.Footer, c.Buttons)
			card.Header = &waE2E.InteractiveMessage_Header{Title: proto.String(c.Title), HasMediaAttachment: proto.Bool(false)}
			carousel.Cards = append(carousel.Cards, card)
		}
		interactive.InteractiveMessage = &waE2E.InteractiveMessage_CarouselMessage_{CarouselMessage: carousel}
	}
	return &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{InteractiveMessage: interactive}}}
}

// interactiveNodes supplies the location-specific or mixed business envelope without an AI marker.
func interactiveNodes(buttonNames ...string) []waBinary.Node {
	nodes := []waBinary.Node{{Tag: "biz", Content: []waBinary.Node{{Tag: "interactive", Attrs: waBinary.Attrs{"type": "native_flow", "v": "1"}, Content: []waBinary.Node{{Tag: "native_flow", Attrs: waBinary.Attrs{"v": "9", "name": "mixed"}}}}}}}
	if len(buttonNames) > 0 && buttonNames[0] == "send_location" {
		nodes[0].Attrs = waBinary.Attrs{"actual_actors": "2", "host_storage": "2", "privacy_mode_ts": strconv.FormatInt(time.Now().Unix(), 10)}
		nodes[0].Content = []waBinary.Node{{Tag: "interactive", Attrs: waBinary.Attrs{"type": "native_flow", "v": "1"}, Content: []waBinary.Node{{Tag: "native_flow", Attrs: waBinary.Attrs{"v": "2", "name": "send_location"}}}}, {Tag: "quality_control", Attrs: waBinary.Attrs{"source_type": "third_party"}}}
	}
	return nodes
}

// SendInteractive validates, uploads carousel images, and sends an immediate experimental message.
func (service serviceSend) SendInteractive(ctx context.Context, req domainSend.InteractiveRequest) (response domainSend.GenericResponse, err error) {
	if err = validations.ValidateInteractive(ctx, req); err != nil {
		return response, err
	}
	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	to, err := utils.ValidateJidWithLogin(client, req.Phone)
	if err != nil {
		return response, err
	}
	if to.Server != types.DefaultUserServer && to.Server != types.HiddenUserServer && to.Server != types.GroupServer {
		return response, pkgError.ValidationError("interactive messages require a user or group recipient")
	}
	name := ""
	if len(req.Buttons) > 0 {
		name = req.Buttons[0].Name
	}
	nodes := interactiveNodes(name)
	msg := buildInteractive(req)
	for index, card := range req.Cards {
		if card.ImageBase64 == "" {
			continue
		}
		header := msg.GetViewOnceMessage().GetMessage().GetInteractiveMessage().GetCarouselMessage().Cards[index].Header
		err = populateInteractiveCardImage(ctx, header, card.ImageBase64, func(ctx context.Context, data []byte) (whatsmeow.UploadResponse, error) {
			return service.uploadMedia(ctx, client, whatsmeow.MediaImage, data, to)
		})
		if err != nil {
			return response, err
		}
	}
	ts, err := service.wrapSendMessage(ctx, client, to, msg, req.Body, whatsmeow.SendRequestExtra{AdditionalNodes: &nodes})
	if err != nil {
		return response, err
	}
	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Experimental interactive message accepted for %s at %s; recipient rendering is unverified", req.Phone, ts.Timestamp.String())
	return response, nil
}

// populateInteractiveCardImage converts an image to JPEG and maps uploaded media into the card header.
// The upload callback keeps media preparation testable without a live WhatsApp connection.
func populateInteractiveCardImage(ctx context.Context, header *waE2E.InteractiveMessage_Header, encoded string, uploadImage func(context.Context, []byte) (whatsmeow.UploadResponse, error)) error {
	data, decodeBase64Err := base64.StdEncoding.DecodeString(encoded)
	if decodeBase64Err != nil {
		return pkgError.ValidationError("invalid base64 card image")
	}
	src, decodeErr := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if decodeErr != nil {
		return pkgError.ValidationError("invalid card image")
	}
	var full, thumb bytes.Buffer
	resized := imaging.Fit(src, 1280, 1280, imaging.Lanczos)
	if err := jpeg.Encode(&full, opaqueInteractiveImage(resized), &jpeg.Options{Quality: 85}); err != nil {
		return err
	}
	if err := jpeg.Encode(&thumb, opaqueInteractiveImage(imaging.Thumbnail(src, 160, 160, imaging.Lanczos)), nil); err != nil {
		return err
	}
	upload, uploadErr := uploadImage(ctx, full.Bytes())
	if uploadErr != nil {
		return uploadErr
	}
	bounds := resized.Bounds()
	header.HasMediaAttachment = proto.Bool(true)
	header.Media = &waE2E.InteractiveMessage_Header_ImageMessage{ImageMessage: &waE2E.ImageMessage{URL: proto.String(upload.URL), DirectPath: proto.String(upload.DirectPath), MediaKey: upload.MediaKey, FileEncSHA256: upload.FileEncSHA256, FileSHA256: upload.FileSHA256, FileLength: proto.Uint64(uint64(full.Len())), Mimetype: proto.String("image/jpeg"), JPEGThumbnail: thumb.Bytes(), Width: proto.Uint32(uint32(bounds.Dx())), Height: proto.Uint32(uint32(bounds.Dy()))}}
	return nil
}

// opaqueInteractiveImage composites transparency onto white before lossy JPEG conversion.
func opaqueInteractiveImage(src image.Image) image.Image {
	dst := image.NewRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
	return dst
}
