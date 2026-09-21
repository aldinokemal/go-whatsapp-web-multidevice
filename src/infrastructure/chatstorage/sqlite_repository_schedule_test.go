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

func TestSQLiteRepositoryListScheduledSendsPaging(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	newJob := func(id, deviceID, status, messageType, summary string, runIn time.Duration) *domainChatStorage.ScheduledSend {
		return &domainChatStorage.ScheduledSend{
			ID: id, DeviceID: deviceID, MessageType: messageType, PayloadJSON: `{"message":"hello"}`,
			Phone: "6281" + id, Summary: summary, ScheduledAt: now.Add(runIn), NextRunAt: now.Add(runIn),
			Timezone: "UTC", Recurrence: "once", WeekdaysJSON: "[]", Status: status,
		}
	}
	require.NoError(t, repo.CreateScheduledSend(newJob("a-1", "device-a", "active", "text", "Promo Jumat", time.Minute)))
	require.NoError(t, repo.CreateScheduledSend(newJob("a-2", "device-a", "active", "image", "Katalog", 2*time.Minute)))
	require.NoError(t, repo.CreateScheduledSend(newJob("a-3", "device-a", "paused", "text", "Reminder", 3*time.Minute)))
	require.NoError(t, repo.CreateScheduledSend(newJob("b-1", "device-b", "active", "text", "Other device", time.Minute)))

	deviceA := domainChatStorage.ScheduledSendFilter{DeviceID: "device-a"}

	total, err := repo.CountScheduledSends(deviceA)
	require.NoError(t, err)
	require.Equal(t, 3, total)

	// The count is what the pager divides into pages, so it has to agree with
	// the list on every predicate rather than counting the whole device.
	activeTotal, err := repo.CountScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", Status: "active"})
	require.NoError(t, err)
	require.Equal(t, 2, activeTotal)

	first, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", Limit: 2})
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, "a-1", first[0].ID)
	require.Equal(t, "a-2", first[1].ID)

	second, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", Limit: 2, Offset: 2})
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, "a-3", second[0].ID)

	// The recurrence worker passes a zero limit and must still see everything.
	all, err := repo.ListScheduledSends(deviceA)
	require.NoError(t, err)
	require.Len(t, all, 3)

	others, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-b", Limit: 25})
	require.NoError(t, err)
	require.Len(t, others, 1)
	require.Equal(t, "b-1", others[0].ID)
}

func TestSQLiteRepositoryListScheduledSendsFilters(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	newJob := func(id, phone, messageType, summary string, runIn time.Duration) *domainChatStorage.ScheduledSend {
		return &domainChatStorage.ScheduledSend{
			ID: id, DeviceID: "device-a", MessageType: messageType, PayloadJSON: `{"message":"hello"}`,
			Phone: phone, Summary: summary, ScheduledAt: now.Add(runIn), NextRunAt: now.Add(runIn),
			Timezone: "UTC", Recurrence: "once", WeekdaysJSON: "[]", Status: "active",
		}
	}
	require.NoError(t, repo.CreateScheduledSend(newJob("s-1", "6281111@s.whatsapp.net", "text", "Promo Jumat", time.Minute)))
	require.NoError(t, repo.CreateScheduledSend(newJob("s-2", "6282222@s.whatsapp.net", "image", "Katalog promo", 2*time.Minute)))
	require.NoError(t, repo.CreateScheduledSend(newJob("s-3", "6283333@s.whatsapp.net", "video", "Reminder", 3*time.Minute)))

	byType, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", MessageType: "image"})
	require.NoError(t, err)
	require.Len(t, byType, 1)
	require.Equal(t, "s-2", byType[0].ID)

	// One term reaches the recipient on one row and the message on another.
	byRecipient, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", Search: "6282222"})
	require.NoError(t, err)
	require.Len(t, byRecipient, 1)
	require.Equal(t, "s-2", byRecipient[0].ID)

	bySummary, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", Search: "promo"})
	require.NoError(t, err)
	require.Len(t, bySummary, 2)

	searchTotal, err := repo.CountScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", Search: "promo"})
	require.NoError(t, err)
	require.Equal(t, 2, searchTotal)

	combined, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a", Search: "promo", MessageType: "text"})
	require.NoError(t, err)
	require.Len(t, combined, 1)
	require.Equal(t, "s-1", combined[0].ID)
}
