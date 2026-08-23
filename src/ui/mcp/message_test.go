package mcp

import (
	"context"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMessageService struct {
	domainMessage.IMessageUsecase
	reacted    *domainMessage.ReactionRequest
	updated    *domainMessage.UpdateMessageRequest
	revoked    *domainMessage.RevokeRequest
	deleted    *domainMessage.DeleteRequest
	marked     *domainMessage.MarkAsReadRequest
	played     *domainMessage.MarkAsPlayedRequest
	starred    *domainMessage.StarRequest
	downloaded *domainMessage.DownloadMediaRequest
}

func (s *stubMessageService) ReactMessage(_ context.Context, r domainMessage.ReactionRequest) (domainMessage.GenericResponse, error) {
	s.reacted = &r
	return domainMessage.GenericResponse{MessageID: r.MessageID}, nil
}
func (s *stubMessageService) UpdateMessage(_ context.Context, r domainMessage.UpdateMessageRequest) (domainMessage.GenericResponse, error) {
	s.updated = &r
	return domainMessage.GenericResponse{MessageID: r.MessageID}, nil
}
func (s *stubMessageService) RevokeMessage(_ context.Context, r domainMessage.RevokeRequest) (domainMessage.GenericResponse, error) {
	s.revoked = &r
	return domainMessage.GenericResponse{MessageID: r.MessageID}, nil
}
func (s *stubMessageService) DeleteMessage(_ context.Context, r domainMessage.DeleteRequest) error {
	s.deleted = &r
	return nil
}
func (s *stubMessageService) MarkAsRead(_ context.Context, r domainMessage.MarkAsReadRequest) (domainMessage.GenericResponse, error) {
	s.marked = &r
	return domainMessage.GenericResponse{MessageID: r.MessageID}, nil
}
func (s *stubMessageService) MarkAsPlayed(_ context.Context, r domainMessage.MarkAsPlayedRequest) (domainMessage.GenericResponse, error) {
	s.played = &r
	return domainMessage.GenericResponse{MessageID: r.MessageID}, nil
}
func (s *stubMessageService) StarMessage(_ context.Context, r domainMessage.StarRequest) error {
	s.starred = &r
	return nil
}
func (s *stubMessageService) DownloadMedia(_ context.Context, r domainMessage.DownloadMediaRequest) (domainMessage.DownloadMediaResponse, error) {
	s.downloaded = &r
	return domainMessage.DownloadMediaResponse{FilePath: "/tmp/x.jpg", MediaType: "image"}, nil
}

func TestHandleMessageDispatch(t *testing.T) {
	base := map[string]any{"phone": "628", "message_id": "M1"}
	withAction := func(action string, extra map[string]any) map[string]any {
		m := map[string]any{"action": action}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	t.Run("react", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		_, err := h.handleMessage(deviceCtx(), callReq(withAction("react", map[string]any{"emoji": "👍"})))
		require.NoError(t, err)
		require.NotNil(t, svc.reacted)
		assert.Equal(t, "👍", svc.reacted.Emoji)
	})

	t.Run("edit", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		_, err := h.handleMessage(deviceCtx(), callReq(withAction("edit", map[string]any{"message": "new"})))
		require.NoError(t, err)
		require.NotNil(t, svc.updated)
		assert.Equal(t, "new", svc.updated.Message)
	})

	t.Run("revoke", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		_, err := h.handleMessage(deviceCtx(), callReq(withAction("revoke", nil)))
		require.NoError(t, err)
		require.NotNil(t, svc.revoked)
	})

	t.Run("delete", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		_, err := h.handleMessage(deviceCtx(), callReq(withAction("delete", nil)))
		require.NoError(t, err)
		require.NotNil(t, svc.deleted)
	})

	t.Run("mark_read", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		_, err := h.handleMessage(deviceCtx(), callReq(withAction("mark_read", nil)))
		require.NoError(t, err)
		require.NotNil(t, svc.marked)
	})

	t.Run("mark_played", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		_, err := h.handleMessage(deviceCtx(), callReq(withAction("mark_played", nil)))
		require.NoError(t, err)
		require.NotNil(t, svc.played)
	})

	t.Run("star and unstar", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		_, err := h.handleMessage(deviceCtx(), callReq(withAction("star", nil)))
		require.NoError(t, err)
		require.NotNil(t, svc.starred)
		assert.True(t, svc.starred.IsStarred)

		_, err = h.handleMessage(deviceCtx(), callReq(withAction("unstar", nil)))
		require.NoError(t, err)
		assert.False(t, svc.starred.IsStarred)
	})

	t.Run("download_media", func(t *testing.T) {
		svc := &stubMessageService{}
		h := InitMcpMessage(svc, &stubResolver{})
		res, err := h.handleMessage(deviceCtx(), callReq(withAction("download_media", nil)))
		require.NoError(t, err)
		require.NotNil(t, svc.downloaded)
		assert.False(t, res.IsError)
	})
}
