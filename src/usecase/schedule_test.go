package usecase

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/stretchr/testify/require"
)

type scheduleRepoStub struct {
	domainChatStorage.IChatStorageRepository
	created *domainChatStorage.ScheduledSend
}

func (r *scheduleRepoStub) CreateScheduledSend(job *domainChatStorage.ScheduledSend) error {
	r.created = job
	return nil
}

type scheduleSendStub struct {
	domainSend.ISendUsecase
}

func TestScheduledSendStoresUserFacingDeviceID(t *testing.T) {
	repo := &scheduleRepoStub{}
	base := &scheduleSendStub{}
	service := NewScheduleService(repo, base, nil, t.TempDir())
	service.now = func() time.Time { return time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC) }
	decorated := NewScheduledSendService(base, service)
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("slot-a", nil, nil))
	response, err := decorated.SendText(ctx, domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone: "628123456789",
			ScheduleOptions: domainSend.ScheduleOptions{
				ScheduledAt: "2026-09-21T11:00:00Z",
				Timezone:    "UTC",
			},
		},
		Message: "hello",
	})
	require.NoError(t, err)
	require.NotEmpty(t, response.ScheduleID)
	require.Equal(t, "slot-a", repo.created.DeviceID)
	require.Equal(t, "text", repo.created.MessageType)
}
