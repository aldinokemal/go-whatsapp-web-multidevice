package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
)

func TestSQLiteRepositoryScheduledSendLifecycle(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	job := &domainChatStorage.ScheduledSend{
		ID: "schedule-1", DeviceID: "device-a", MessageType: "text", PayloadJSON: `{"message":"hello"}`,
		Phone: "6281", Summary: "hello", ScheduledAt: now.Add(-time.Minute), NextRunAt: now.Add(-time.Minute),
		Timezone: "UTC", Recurrence: "once", WeekdaysJSON: "[]", Status: "active",
	}
	require.NoError(t, repo.CreateScheduledSend(job))

	claimed, err := repo.ClaimDueScheduledSends(now, 10, now.Add(time.Minute), "lease-a")
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "running", claimed[0].Status)
	require.NoError(t, repo.CompleteScheduledSend(job.ID, "lease-a", "completed", "message-1", 1, nil))

	stored, err := repo.GetScheduledSend("device-a", job.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", stored.Status)
	require.Equal(t, "message-1", stored.LastMessageID)
	require.Equal(t, 1, stored.OccurrenceCount)

	other, err := repo.GetScheduledSend("device-b", job.ID)
	require.NoError(t, err)
	require.Nil(t, other)
}
