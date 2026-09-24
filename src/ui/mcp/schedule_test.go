package mcp

import (
	"context"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubScheduleService struct {
	domainSend.IScheduleUsecase
	lastID string
}

func (s *stubScheduleService) Pause(_ context.Context, id string) error {
	s.lastID = id
	return nil
}
func (s *stubScheduleService) Resume(_ context.Context, id string) error {
	s.lastID = id
	return nil
}
func (s *stubScheduleService) Cancel(_ context.Context, id string) error {
	s.lastID = id
	return nil
}

func TestHandleScheduleTransitions(t *testing.T) {
	for action, want := range map[string]string{
		"pause":  "Schedule S1 paused",
		"resume": "Schedule S1 resumed",
		"cancel": "Schedule S1 cancelled",
	} {
		t.Run(action, func(t *testing.T) {
			svc := &stubScheduleService{}
			h := InitMcpSchedule(svc, &stubResolver{})
			res, err := h.handle(deviceCtx(), callReq(map[string]any{"action": action, "schedule_id": "S1"}))
			require.NoError(t, err)
			require.False(t, res.IsError)
			assert.Equal(t, "S1", svc.lastID)
			assert.Equal(t, want, resultText(t, res))
		})
	}
}
