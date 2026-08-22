package mcp

import (
	"context"
	"errors"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSendService embeds the interface so only overridden methods matter;
// an unexpected call panics, which is the failure we want in a dispatch test.
type stubSendService struct {
	domainSend.ISendUsecase
	lastText     *domainSend.MessageRequest
	lastImage    *domainSend.ImageRequest
	lastVideo    *domainSend.VideoRequest
	lastAudio    *domainSend.AudioRequest
	lastFile     *domainSend.FileRequest
	lastSticker  *domainSend.StickerRequest
	lastLocation *domainSend.LocationRequest
	lastContact  *domainSend.ContactRequest
	lastPoll     *domainSend.PollRequest
	lastLink     *domainSend.LinkRequest
	lastForward  *domainSend.ForwardRequest
	err          error
}

func (s *stubSendService) resp() (domainSend.GenericResponse, error) {
	if s.err != nil {
		return domainSend.GenericResponse{}, s.err
	}
	return domainSend.GenericResponse{MessageID: "MSG1"}, nil
}

func (s *stubSendService) SendText(_ context.Context, r domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	s.lastText = &r
	return s.resp()
}
func (s *stubSendService) SendImage(_ context.Context, r domainSend.ImageRequest) (domainSend.GenericResponse, error) {
	s.lastImage = &r
	return s.resp()
}
func (s *stubSendService) SendVideo(_ context.Context, r domainSend.VideoRequest) (domainSend.GenericResponse, error) {
	s.lastVideo = &r
	return s.resp()
}
func (s *stubSendService) SendAudio(_ context.Context, r domainSend.AudioRequest) (domainSend.GenericResponse, error) {
	s.lastAudio = &r
	return s.resp()
}
func (s *stubSendService) SendFile(_ context.Context, r domainSend.FileRequest) (domainSend.GenericResponse, error) {
	s.lastFile = &r
	return s.resp()
}
func (s *stubSendService) SendSticker(_ context.Context, r domainSend.StickerRequest) (domainSend.GenericResponse, error) {
	s.lastSticker = &r
	return s.resp()
}
func (s *stubSendService) SendLocation(_ context.Context, r domainSend.LocationRequest) (domainSend.GenericResponse, error) {
	s.lastLocation = &r
	return s.resp()
}
func (s *stubSendService) SendContact(_ context.Context, r domainSend.ContactRequest) (domainSend.GenericResponse, error) {
	s.lastContact = &r
	return s.resp()
}
func (s *stubSendService) SendPoll(_ context.Context, r domainSend.PollRequest) (domainSend.GenericResponse, error) {
	s.lastPoll = &r
	return s.resp()
}
func (s *stubSendService) SendLink(_ context.Context, r domainSend.LinkRequest) (domainSend.GenericResponse, error) {
	s.lastLink = &r
	return s.resp()
}
func (s *stubSendService) SendForward(_ context.Context, r domainSend.ForwardRequest) (domainSend.GenericResponse, error) {
	s.lastForward = &r
	return s.resp()
}

// deviceCtx returns a context that already carries a device, as the HTTP
// layer would after resolving X-Device-Id.
func deviceCtx() context.Context {
	return whatsapp.ContextWithDevice(context.Background(), &whatsapp.DeviceInstance{})
}

func TestHandleSendDispatch(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "text", "phone": "628", "message": "hi",
			"reply_message_id": "R1", "mentions": []any{"629"}, "is_forwarded": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.IsError)
		require.NotNil(t, svc.lastText)
		assert.Equal(t, "628", svc.lastText.Phone)
		assert.Equal(t, "hi", svc.lastText.Message)
		assert.Equal(t, "R1", *svc.lastText.ReplyMessageID)
		assert.Equal(t, []string{"629"}, svc.lastText.Mentions)
		assert.True(t, svc.lastText.IsForwarded)
	})

	t.Run("image", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "image", "phone": "628", "image_url": "http://x/a.png",
			"caption": "c", "view_once": true, "hd": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastImage)
		assert.Equal(t, "http://x/a.png", *svc.lastImage.ImageURL)
		assert.True(t, svc.lastImage.ViewOnce)
		assert.True(t, svc.lastImage.Compress) // image compress defaults true
		assert.True(t, svc.lastImage.HD)
	})

	t.Run("video", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "video", "phone": "628", "video_url": "http://x/a.mp4", "gif_playback": true, "hd": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastVideo)
		assert.True(t, svc.lastVideo.GifPlayback)
		assert.False(t, svc.lastVideo.Compress) // video compress defaults false
		assert.True(t, svc.lastVideo.HD)
	})

	t.Run("audio", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "audio", "phone": "628", "audio_url": "http://x/a.ogg", "ptt": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastAudio)
		assert.True(t, svc.lastAudio.PTT)
	})

	t.Run("document", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "document", "phone": "628", "file_url": "http://x/a.pdf",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastFile)
	})

	t.Run("sticker", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "sticker", "phone": "628", "sticker_url": "http://x/a.png",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastSticker)
	})

	t.Run("location", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "location", "phone": "628", "latitude": "-6.2", "longitude": "106.8",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastLocation)
	})

	t.Run("contact", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "contact", "phone": "628", "contact_name": "A", "contact_phone": "629",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastContact)
	})

	t.Run("poll", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "poll", "phone": "628", "question": "q", "options": []any{"a", "b"},
			"max_answer": 2, "is_forwarded": true,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastPoll)
		assert.Equal(t, 2, svc.lastPoll.MaxAnswer)
		assert.Equal(t, "628", svc.lastPoll.Phone)
		assert.True(t, svc.lastPoll.IsForwarded)
	})

	t.Run("link", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "link", "phone": "628", "link": "http://x", "caption": "c",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastLink)
	})

	t.Run("forward with duration", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "forward", "phone": "628", "message_id": "M1", "duration": 86400,
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastForward)
		require.NotNil(t, svc.lastForward.Duration)
		assert.Equal(t, 86400, *svc.lastForward.Duration)
	})

	t.Run("usecase error becomes tool error", func(t *testing.T) {
		svc := &stubSendService{err: errors.New("boom")}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "text", "phone": "628", "message": "hi",
		}))
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.True(t, res.IsError)
	})

	t.Run("no device is a tool error", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(context.Background(), callReq(map[string]any{
			"type": "text", "phone": "628", "message": "hi",
		}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})
}
