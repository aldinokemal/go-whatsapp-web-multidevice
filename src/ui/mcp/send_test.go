package mcp

import (
	"context"
	"errors"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	mcpg "github.com/mark3labs/mcp-go/mcp"
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
	lastInteract *domainSend.InteractiveRequest
	lastPresence *domainSend.PresenceRequest
	lastChatPres *domainSend.ChatPresenceRequest
	scheduled    bool
	err          error
}

func (s *stubSendService) resp() (domainSend.GenericResponse, error) {
	if s.err != nil {
		return domainSend.GenericResponse{}, s.err
	}
	if s.scheduled {
		return domainSend.GenericResponse{Status: "Message scheduled", ScheduleID: "S1", ScheduledAt: "2026-09-22T10:00:00Z", NextRunAt: "2026-09-22T10:00:00Z"}, nil
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
func (s *stubSendService) SendInteractive(_ context.Context, r domainSend.InteractiveRequest) (domainSend.GenericResponse, error) {
	s.lastInteract = &r
	res, err := s.resp()
	res.Status = "accepted; recipient rendering is unverified"
	return res, err
}
func (s *stubSendService) SendPresence(_ context.Context, r domainSend.PresenceRequest) (domainSend.GenericResponse, error) {
	s.lastPresence = &r
	res, err := s.resp()
	res.Status = "Send presence success " + r.Type
	return res, err
}
func (s *stubSendService) SendChatPresence(_ context.Context, r domainSend.ChatPresenceRequest) (domainSend.GenericResponse, error) {
	s.lastChatPres = &r
	res, err := s.resp()
	res.Status = "Send chat presence " + r.Action + " typing success " + r.Phone
	return res, err
}

// resultText returns the text content a tool result reports to the client.
func resultText(t *testing.T, res *mcpg.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(mcpg.TextContent)
	require.True(t, ok)
	return text.Text
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
		assert.Equal(t, "628@s.whatsapp.net", svc.lastText.Phone)
		assert.Equal(t, "hi", svc.lastText.Message)
		assert.Equal(t, "R1", *svc.lastText.ReplyMessageID)
		assert.Equal(t, []string{"629"}, svc.lastText.Mentions)
		assert.True(t, svc.lastText.IsForwarded)
		assert.Nil(t, svc.lastText.Duration) // absent duration must not become an explicit 0
	})

	t.Run("reply_message_id and duration reach every type that supports them", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		for _, args := range []map[string]any{
			{"type": "text", "message": "hi"},
			{"type": "image", "image_url": "http://x/a.png"},
			{"type": "video", "video_url": "http://x/a.mp4"},
			{"type": "audio", "audio_url": "http://x/a.ogg"},
			{"type": "document", "file_url": "http://x/a.pdf"},
			{"type": "sticker", "sticker_url": "http://x/a.png"},
			{"type": "location", "latitude": "-6.2", "longitude": "106.8"},
			{"type": "contact", "contact_name": "A", "contact_phone": "629"},
			{"type": "poll", "question": "q", "options": []any{"a", "b"}},
			{"type": "link", "link": "http://x", "caption": "c"},
		} {
			args["phone"] = "628"
			args["reply_message_id"] = "R1"
			args["duration"] = float64(0)
			res, err := h.handleSend(deviceCtx(), callReq(args))
			require.NoError(t, err)
			require.False(t, res.IsError, args["type"])
		}
		for name, got := range map[string]*string{
			"text": svc.lastText.ReplyMessageID, "image": svc.lastImage.ReplyMessageID,
			"video": svc.lastVideo.ReplyMessageID, "audio": svc.lastAudio.ReplyMessageID,
			"document": svc.lastFile.ReplyMessageID, "sticker": svc.lastSticker.ReplyMessageID,
		} {
			require.NotNil(t, got, name)
			assert.Equal(t, "R1", *got, name)
		}
		for name, got := range map[string]*int{
			"text": svc.lastText.Duration, "image": svc.lastImage.Duration, "video": svc.lastVideo.Duration,
			"audio": svc.lastAudio.Duration, "document": svc.lastFile.Duration, "sticker": svc.lastSticker.Duration,
			"location": svc.lastLocation.Duration, "contact": svc.lastContact.Duration,
			"poll": svc.lastPoll.Duration, "link": svc.lastLink.Duration,
		} {
			require.NotNil(t, got, name) // explicit 0 turns expiry off, so it must survive as a pointer
			assert.Equal(t, 0, *got, name)
		}
	})

	t.Run("presence needs no phone", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{"type": "presence", "presence": "unavailable"}))
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.NotNil(t, svc.lastPresence)
		assert.Equal(t, "unavailable", svc.lastPresence.Type)
		assert.Equal(t, "Send presence success unavailable", resultText(t, res))
	})

	t.Run("presence cannot be scheduled", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "presence", "presence": "available", "scheduled_at": "2026-09-22T10:00:00Z", "timezone": "UTC",
		}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Nil(t, svc.lastPresence)
	})

	t.Run("presence usecase error becomes tool error", func(t *testing.T) {
		h := InitMcpSend(&stubSendService{err: errors.New("boom")}, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{"type": "presence", "presence": "available"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Equal(t, "boom", resultText(t, res))
	})

	t.Run("chat_presence", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "chat_presence", "phone": "628", "action": "start",
			"scheduled_at": "2026-09-22T10:00:00Z", "timezone": "UTC",
		}))
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.NotNil(t, svc.lastChatPres)
		assert.Equal(t, "628@s.whatsapp.net", svc.lastChatPres.Phone)
		assert.Equal(t, "start", svc.lastChatPres.Action)
		// The scheduling decorator rejects chat presence by reading these options.
		assert.True(t, svc.lastChatPres.IsScheduled())
		assert.Equal(t, "Send chat presence start typing success 628@s.whatsapp.net", resultText(t, res))
	})

	t.Run("chat_presence usecase error becomes tool error", func(t *testing.T) {
		h := InitMcpSend(&stubSendService{err: errors.New("boom")}, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{"type": "chat_presence", "phone": "628", "action": "stop"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Equal(t, "boom", resultText(t, res))
	})

	t.Run("image", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "image", "phone": "628", "image_url": "http://x/a.png",
			"caption": "c", "view_once": true, "hd": true, "mentions": []any{"629", "@everyone"},
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastImage)
		assert.Equal(t, "http://x/a.png", *svc.lastImage.ImageURL)
		assert.Equal(t, []string{"629", "@everyone"}, svc.lastImage.Mentions)
		assert.True(t, svc.lastImage.ViewOnce)
		assert.True(t, svc.lastImage.Compress) // image compress defaults true
		assert.True(t, svc.lastImage.HD)
	})

	t.Run("video", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "video", "phone": "628", "video_url": "http://x/a.mp4", "gif_playback": true, "hd": true,
			"mentions": []any{"629"},
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastVideo)
		assert.Equal(t, []string{"629"}, svc.lastVideo.Mentions)
		assert.True(t, svc.lastVideo.GifPlayback)
		assert.False(t, svc.lastVideo.Compress) // video compress defaults false
		assert.True(t, svc.lastVideo.HD)
	})

	t.Run("allow_reshare", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		for _, args := range []map[string]any{
			{"type": "text", "message": "hi"},
			{"type": "image", "image_url": "http://x/a.png"},
			{"type": "video", "video_url": "http://x/a.mp4"},
		} {
			args["phone"] = "status@broadcast"
			args["allow_reshare"] = true
			_, err := h.handleSend(deviceCtx(), callReq(args))
			require.NoError(t, err)
		}
		assert.True(t, svc.lastText.AllowReshare)
		assert.True(t, svc.lastImage.AllowReshare)
		assert.True(t, svc.lastVideo.AllowReshare)
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
			"type": "document", "phone": "628", "file_url": "http://x/a.pdf", "mentions": []any{"629"},
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastFile)
		assert.Equal(t, []string{"629"}, svc.lastFile.Mentions)
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
		assert.Equal(t, "628@s.whatsapp.net", svc.lastPoll.Phone)
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

	t.Run("phone is sanitized like REST", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		for phone, want := range map[string]string{
			"120363025246125486":      "120363025246125486@g.us",
			"status@broadcast":        "status@broadcast",
			"628@s.whatsapp.net":      "628@s.whatsapp.net",
			"120363025246125486@g.us": "120363025246125486@g.us",
		} {
			_, err := h.handleSend(deviceCtx(), callReq(map[string]any{"type": "text", "phone": phone, "message": "hi"}))
			require.NoError(t, err)
			assert.Equal(t, want, svc.lastText.Phone, phone)
		}
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{"type": "forward", "phone": "120363025246125486", "message_id": "M1"}))
		require.NoError(t, err)
		assert.Equal(t, "120363025246125486@g.us", svc.lastForward.Phone)
	})

	t.Run("interactive buttons", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "interactive", "phone": "628", "body": "Confirm?", "footer": "f",
			"buttons": []any{map[string]any{"name": "quick_reply", "params": map[string]any{"display_text": "Yes", "id": "yes"}}},
		}))
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.NotNil(t, svc.lastInteract)
		assert.Equal(t, "628@s.whatsapp.net", svc.lastInteract.Phone)
		assert.Equal(t, "Confirm?", svc.lastInteract.Body)
		assert.Equal(t, "f", svc.lastInteract.Footer)
		require.Len(t, svc.lastInteract.Buttons, 1)
		assert.Equal(t, "quick_reply", svc.lastInteract.Buttons[0].Name)
		assert.JSONEq(t, `{"display_text":"Yes","id":"yes"}`, string(svc.lastInteract.Buttons[0].Params))
		assert.Equal(t, "accepted; recipient rendering is unverified (message ID MSG1)", resultText(t, res))
	})

	t.Run("interactive cards", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		card := func(title string) map[string]any {
			return map[string]any{"title": title, "body": "b", "image_base64": "aGk=",
				"buttons": []any{map[string]any{"name": "quick_reply", "params": map[string]any{"display_text": title, "id": title}}}}
		}
		_, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "interactive", "phone": "628", "body": "Pick", "cards": []any{card("A"), card("B")},
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.lastInteract)
		require.Len(t, svc.lastInteract.Cards, 2)
		assert.Equal(t, "A", svc.lastInteract.Cards[0].Title)
		assert.Equal(t, "aGk=", svc.lastInteract.Cards[0].ImageBase64)
		assert.JSONEq(t, `{"display_text":"B","id":"B"}`, string(svc.lastInteract.Cards[1].Buttons[0].Params))
	})

	t.Run("interactive usecase error becomes tool error", func(t *testing.T) {
		svc := &stubSendService{err: errors.New("boom")}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "interactive", "phone": "628", "body": "x",
			"buttons": []any{map[string]any{"name": "quick_reply", "params": map[string]any{"display_text": "Yes", "id": "yes"}}},
		}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Equal(t, "boom", resultText(t, res))
	})

	t.Run("interactive cannot be scheduled", func(t *testing.T) {
		svc := &stubSendService{}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "interactive", "phone": "628", "body": "x",
			"buttons":      []any{map[string]any{"name": "quick_reply", "params": map[string]any{"display_text": "Yes", "id": "yes"}}},
			"scheduled_at": "2026-09-22T10:00:00Z", "timezone": "UTC",
		}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
		assert.Nil(t, svc.lastInteract)
	})

	t.Run("scheduled send reports schedule_id", func(t *testing.T) {
		svc := &stubSendService{scheduled: true}
		h := InitMcpSend(svc, &stubResolver{})
		res, err := h.handleSend(deviceCtx(), callReq(map[string]any{
			"type": "text", "phone": "628", "message": "hi",
			"scheduled_at": "2026-09-22T10:00:00Z", "timezone": "UTC",
			"recurrence": "weekly", "weekdays": []any{float64(1), float64(3)},
		}))
		require.NoError(t, err)
		require.False(t, res.IsError)
		require.NotNil(t, svc.lastText)
		assert.Equal(t, []int{1, 3}, svc.lastText.Weekdays)
		assert.Equal(t, "text scheduled with schedule_id S1, next run 2026-09-22T10:00:00Z", resultText(t, res))
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
