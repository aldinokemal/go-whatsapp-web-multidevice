package mcp

import (
	"context"
	"testing"

	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubLabelService struct {
	listed   bool
	resynced bool
	edited   *domainLabel.EditLabelRequest
	labeled  *domainLabel.LabelChatRequest
	listResp domainLabel.ListLabelsResponse
}

func (s *stubLabelService) ListLabels(context.Context) (domainLabel.ListLabelsResponse, error) {
	s.listed = true
	return s.listResp, nil
}
func (s *stubLabelService) ResyncLabels(context.Context) (domainLabel.ListLabelsResponse, error) {
	s.resynced = true
	return s.listResp, nil
}
func (s *stubLabelService) EditLabel(_ context.Context, r domainLabel.EditLabelRequest) (domainLabel.EditLabelResponse, error) {
	s.edited = &r
	return domainLabel.EditLabelResponse{Message: "saved"}, nil
}
func (s *stubLabelService) LabelChat(_ context.Context, r domainLabel.LabelChatRequest) (domainLabel.LabelChatResponse, error) {
	s.labeled = &r
	return domainLabel.LabelChatResponse{Message: "labeled"}, nil
}

func TestHandleLabelDispatch(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		svc := &stubLabelService{listResp: domainLabel.ListLabelsResponse{Data: []domainLabel.LabelInfo{{ID: "5", Name: "Paid", Chats: []string{}}}}}
		res, err := InitMcpLabel(svc, &stubResolver{}).handleLabel(deviceCtx(), callReq(map[string]any{"action": "list"}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.True(t, svc.listed)
		assert.NotNil(t, res.StructuredContent)
	})

	t.Run("resync", func(t *testing.T) {
		svc := &stubLabelService{}
		res, err := InitMcpLabel(svc, &stubResolver{}).handleLabel(deviceCtx(), callReq(map[string]any{"action": "resync"}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
		assert.True(t, svc.resynced)
	})

	t.Run("edit with optional fields", func(t *testing.T) {
		svc := &stubLabelService{}
		res, err := InitMcpLabel(svc, &stubResolver{}).handleLabel(deviceCtx(), callReq(map[string]any{
			"action": "edit", "label_id": "30", "name": "Family", "type": "CUSTOM", "color": float64(0),
		}))
		require.NoError(t, err)
		assert.False(t, res.IsError)
		require.NotNil(t, svc.edited)
		assert.Equal(t, "30", svc.edited.LabelID)
		assert.Equal(t, "Family", svc.edited.Name)
		assert.Equal(t, "CUSTOM", svc.edited.Type)
		require.NotNil(t, svc.edited.Color, "an explicit 0 color must reach the service")
		assert.Equal(t, int32(0), *svc.edited.Color)
		assert.Nil(t, svc.edited.OrderIndex)
		assert.False(t, svc.edited.Deleted)
	})

	t.Run("chat", func(t *testing.T) {
		for _, labeled := range []bool{true, false} {
			svc := &stubLabelService{}
			res, err := InitMcpLabel(svc, &stubResolver{}).handleLabel(deviceCtx(), callReq(map[string]any{
				"action": "chat", "label_id": "30", "chat_jid": "628123456789", "labeled": labeled,
			}))
			require.NoError(t, err)
			assert.False(t, res.IsError)
			require.NotNil(t, svc.labeled)
			assert.Equal(t, "628123456789@s.whatsapp.net", svc.labeled.ChatJID)
			require.NotNil(t, svc.labeled.Labeled)
			assert.Equal(t, labeled, *svc.labeled.Labeled)
		}
	})

	t.Run("chat without the flag", func(t *testing.T) {
		svc := &stubLabelService{}
		_, err := InitMcpLabel(svc, &stubResolver{}).handleLabel(deviceCtx(), callReq(map[string]any{
			"action": "chat", "label_id": "30", "chat_jid": "628123456789@s.whatsapp.net",
		}))
		require.NoError(t, err)
		require.NotNil(t, svc.labeled)
		assert.Nil(t, svc.labeled.Labeled, "a missing flag must reach validation as nil, not as a removal")
	})

	t.Run("unknown action", func(t *testing.T) {
		res, err := InitMcpLabel(&stubLabelService{}, &stubResolver{}).handleLabel(deviceCtx(), callReq(map[string]any{"action": "nuke"}))
		require.NoError(t, err)
		assert.True(t, res.IsError)
	})
}
