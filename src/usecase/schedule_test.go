package usecase

import (
	"context"
	"os"
	"path/filepath"
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

func TestHydrateAssetPreservesContentType(t *testing.T) {
	cases := []struct {
		name  string
		asset scheduledAsset
		want  string
	}{
		{
			name:  "uses stored content type",
			asset: scheduledAsset{Filename: "photo.png", ContentType: "image/png"},
			want:  "image/png",
		},
		{
			name:  "falls back to the filename extension",
			asset: scheduledAsset{Filename: "photo.jpg"},
			want:  "image/jpeg",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.asset.Filename)
			require.NoError(t, os.WriteFile(path, []byte("media-bytes"), 0o600))
			asset := tc.asset
			asset.Path = path

			hydrated, err := hydrateAsset(asset, "image")
			require.NoError(t, err)
			require.NotNil(t, hydrated)
			defer hydrated.cleanup()

			require.Equal(t, tc.want, hydrated.Header.Header.Get("Content-Type"))
			require.Equal(t, tc.asset.Filename, hydrated.Header.Filename)
		})
	}
}
