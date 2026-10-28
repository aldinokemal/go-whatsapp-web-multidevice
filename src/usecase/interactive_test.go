package usecase

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// TestBuildInteractiveLocation protects the client-confirmed unwrapped location payload.
func TestBuildInteractiveLocation(t *testing.T) {
	msg := buildInteractive(domainSend.InteractiveRequest{Body: "Share location", Buttons: []domainSend.NativeFlowButton{{Name: "send_location", Params: json.RawMessage(`{}`)}}})
	require.Nil(t, msg.ViewOnceMessage)
	i := msg.GetInteractiveMessage()
	require.NotNil(t, i)
	require.NotNil(t, i.Header)
	require.False(t, i.Header.GetHasMediaAttachment())
	require.Nil(t, i.GetNativeFlowMessage().MessageVersion)
	require.Equal(t, "send_location", i.GetNativeFlowMessage().Buttons[0].GetName())
	require.NotNil(t, msg.GetMessageContextInfo().DeviceListMetadata)
	require.EqualValues(t, 2, msg.GetMessageContextInfo().GetDeviceListMetadataVersion())
	require.Len(t, msg.GetMessageContextInfo().GetMessageSecret(), 32)
	_, err := proto.Marshal(msg)
	require.NoError(t, err)
}

// TestPopulateInteractiveCardImage verifies upload bytes, media mapping, and failure propagation.
func TestPopulateInteractiveCardImage(t *testing.T) {
	var source bytes.Buffer
	require.NoError(t, png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 1600, 800))))
	encoded := base64.StdEncoding.EncodeToString(source.Bytes())
	header := &waE2E.InteractiveMessage_Header{}
	upload := whatsmeow.UploadResponse{URL: "https://example.com/image", DirectPath: "/image", MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}}
	var uploaded []byte
	err := populateInteractiveCardImage(context.Background(), header, encoded, func(_ context.Context, data []byte) (whatsmeow.UploadResponse, error) {
		uploaded = append([]byte(nil), data...)
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, "jpeg", format)
		require.Equal(t, 1280, cfg.Width)
		require.Equal(t, 640, cfg.Height)
		return upload, nil
	})
	require.NoError(t, err)
	require.True(t, header.GetHasMediaAttachment())
	media := header.GetImageMessage()
	require.Equal(t, upload.URL, media.GetURL())
	require.Equal(t, upload.DirectPath, media.GetDirectPath())
	require.Equal(t, upload.MediaKey, media.MediaKey)
	require.Equal(t, upload.FileSHA256, media.FileSHA256)
	require.Equal(t, upload.FileEncSHA256, media.FileEncSHA256)
	require.EqualValues(t, len(uploaded), media.GetFileLength())
	require.EqualValues(t, 1280, media.GetWidth())
	require.EqualValues(t, 640, media.GetHeight())
	require.Equal(t, "image/jpeg", media.GetMimetype())
	require.NotEmpty(t, media.JPEGThumbnail)
	failure := errors.New("upload failed")
	callback := func(context.Context, []byte) (whatsmeow.UploadResponse, error) {
		return whatsmeow.UploadResponse{}, failure
	}
	require.ErrorIs(t, populateInteractiveCardImage(context.Background(), header, encoded, callback), failure)
	for _, invalid := range []string{"!", base64.StdEncoding.EncodeToString([]byte("not an image"))} {
		require.Error(t, populateInteractiveCardImage(context.Background(), header, invalid, func(context.Context, []byte) (whatsmeow.UploadResponse, error) {
			t.Fatal("invalid image must not upload")
			return whatsmeow.UploadResponse{}, nil
		}))
	}
}

func TestBuildInteractive(t *testing.T) {
	b := domainSend.NativeFlowButton{Name: "quick_reply", Params: json.RawMessage(`{"display_text":"Confirm","id":"confirm"}`)}
	req := domainSend.InteractiveRequest{Body: "Choose", Buttons: []domainSend.NativeFlowButton{b}}
	msg := buildInteractive(req)
	_, err := proto.Marshal(msg)
	require.NoError(t, err)
	i := msg.GetViewOnceMessage().GetMessage().GetInteractiveMessage()
	require.Equal(t, "Choose", i.GetBody().GetText())
	require.Equal(t, "quick_reply", i.GetNativeFlowMessage().GetButtons()[0].GetName())
	req.Buttons = nil
	req.Cards = []domainSend.InteractiveCard{{Title: "One", Body: "A", Buttons: []domainSend.NativeFlowButton{b}}, {Title: "Two", Body: "B", Buttons: []domainSend.NativeFlowButton{b}}}
	i = buildInteractive(req).GetViewOnceMessage().GetMessage().GetInteractiveMessage()
	require.Nil(t, i.GetNativeFlowMessage())
	require.Len(t, i.GetCarouselMessage().GetCards(), 2)
	require.Equal(t, "Two", i.GetCarouselMessage().GetCards()[1].GetHeader().GetTitle())
}

func TestInteractiveNodes(t *testing.T) {
	location := interactiveNodes("send_location")
	require.Equal(t, "send_location", location[0].GetChildren()[0].GetChildren()[0].Attrs["name"])
	require.Equal(t, "2", location[0].GetChildren()[0].GetChildren()[0].Attrs["v"])
	private := interactiveNodes()
	require.Len(t, private, 1)
	require.Equal(t, "biz", private[0].Tag)
	interactive := private[0].GetChildren()[0]
	require.Equal(t, "native_flow", interactive.Attrs["type"])
	require.Equal(t, "mixed", interactive.GetChildren()[0].Attrs["name"])
	require.Len(t, interactiveNodes(), 1)
}
