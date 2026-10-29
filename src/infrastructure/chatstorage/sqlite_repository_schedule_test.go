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

	claimed, err := repo.ClaimNextScheduledSend(now, now.Add(time.Minute), "lease-a")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, "running", claimed.Status)
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

	// A zero limit still returns every row.
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

func newScheduledSendFixture(id, deviceID, status string, nextRunAt time.Time) *domainChatStorage.ScheduledSend {
	return &domainChatStorage.ScheduledSend{
		ID: id, DeviceID: deviceID, MessageType: "text", PayloadJSON: `{"message":"hello"}`,
		Phone: "6281", Summary: id, ScheduledAt: nextRunAt, NextRunAt: nextRunAt,
		Timezone: "UTC", Recurrence: "once", WeekdaysJSON: "[]", Status: status,
	}
}

func TestSQLiteRepositoryClaimNextScheduledSendClaimsOneDueJob(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("later", "device-a", "active", now.Add(-time.Minute))))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("earlier", "device-a", "active", now.Add(-2*time.Minute))))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("future", "device-a", "active", now.Add(time.Minute))))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("paused", "device-a", "paused", now.Add(-3*time.Minute))))

	first, err := repo.ClaimNextScheduledSend(now, now.Add(time.Minute), "lease-1")
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, "earlier", first.ID)
	require.Equal(t, "running", first.Status)
	require.Equal(t, "lease-1", first.LeaseToken)
	require.NotNil(t, first.LeaseUntil)

	stored, err := repo.GetScheduledSend("device-a", "later")
	require.NoError(t, err)
	require.Equal(t, "active", stored.Status, "only one job is claimed per call")

	second, err := repo.ClaimNextScheduledSend(now, now.Add(time.Minute), "lease-2")
	require.NoError(t, err)
	require.NotNil(t, second)
	require.Equal(t, "later", second.ID)

	none, err := repo.ClaimNextScheduledSend(now, now.Add(time.Minute), "lease-3")
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestSQLiteRepositoryScheduledSendLeaseGuards(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("job", "device-a", "active", now.Add(-time.Minute))))
	_, err := repo.ClaimNextScheduledSend(now, now.Add(time.Minute), "lease-a")
	require.NoError(t, err)

	require.NoError(t, repo.CompleteScheduledSend("job", "stale", "completed", "message-1", 1, nil))
	require.NoError(t, repo.FailScheduledSend("job", "stale", "boom"))
	require.NoError(t, repo.RetryScheduledSend("job", "stale", "boom", 5, now))
	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, "running", stored.Status, "a stale lease token must not change the job")
	require.Zero(t, stored.Attempts)

	require.NoError(t, repo.RetryScheduledSend("job", "lease-a", "timeout", 3, now.Add(-time.Second)))
	stored, err = repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, "active", stored.Status)
	require.Equal(t, 3, stored.Attempts)
	require.Equal(t, "timeout", stored.LastError)
	require.Empty(t, stored.LeaseToken)

	_, err = repo.ClaimNextScheduledSend(now, now.Add(time.Minute), "lease-b")
	require.NoError(t, err)
	require.NoError(t, repo.CompleteScheduledSend("job", "lease-b", "completed", "message-1", 1, nil))
	stored, err = repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, "completed", stored.Status)
	require.Zero(t, stored.Attempts, "completing resets the attempt budget")
}

func TestSQLiteRepositorySetScheduledSendStatusGuardsTransitions(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		deviceID string
		from     []string
		to       string
		changed  bool
	}{
		{name: "pause active", current: "active", from: []string{"active"}, to: "paused", changed: true},
		{name: "pause running rejected", current: "running", from: []string{"active"}, to: "paused"},
		{name: "pause failed rejected", current: "failed", from: []string{"active"}, to: "paused"},
		{name: "resume paused", current: "paused", from: []string{"paused"}, to: "active", changed: true},
		{name: "resume active rejected", current: "active", from: []string{"paused"}, to: "active"},
		{name: "cancel failed", current: "failed", from: []string{"active", "paused", "failed"}, to: "cancelled", changed: true},
		{name: "cancel running rejected", current: "running", from: []string{"active", "paused", "failed"}, to: "cancelled"},
		{name: "cancel completed rejected", current: "completed", from: []string{"active", "paused", "failed"}, to: "cancelled"},
		{name: "other device rejected", current: "active", deviceID: "device-b", from: []string{"active"}, to: "paused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("job", "device-a", tt.current, time.Now().UTC().Add(time.Hour))))
			deviceID := tt.deviceID
			if deviceID == "" {
				deviceID = "device-a"
			}

			changed, err := repo.SetScheduledSendStatus(deviceID, "job", tt.from, tt.to, nil)
			require.NoError(t, err)
			require.Equal(t, tt.changed, changed)

			stored, err := repo.GetScheduledSend("device-a", "job")
			require.NoError(t, err)
			want := tt.current
			if tt.changed {
				want = tt.to
			}
			require.Equal(t, want, stored.Status)
		})
	}
}

func TestSQLiteRepositoryListExpiredScheduledSends(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	expired := newScheduledSendFixture("expired", "device-a", "running", now.Add(-time.Hour))
	expired.LeaseToken, expired.LeaseUntil = "lease-a", timePointer(now.Add(-time.Minute))
	leased := newScheduledSendFixture("leased", "device-a", "running", now.Add(-time.Hour))
	leased.LeaseToken, leased.LeaseUntil = "lease-b", timePointer(now.Add(time.Minute))
	require.NoError(t, repo.CreateScheduledSend(expired))
	require.NoError(t, repo.CreateScheduledSend(leased))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("active", "device-a", "active", now.Add(-time.Hour))))

	jobs, err := repo.ListExpiredScheduledSends(now)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, "expired", jobs[0].ID)
	require.Equal(t, "lease-a", jobs[0].LeaseToken)
}

func TestSQLiteRepositoryListScheduledSendIDsReturnsLiveJobs(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC()
	for _, status := range []string{"active", "running", "paused", "completed", "failed", "cancelled"} {
		require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture(status, "device-"+status, status, now)))
	}

	ids, err := repo.ListScheduledSendIDs()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"active", "running", "paused"}, ids)
}

func TestSQLiteRepositoryDeviceCleanupRemovesScheduledSends(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC()
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("a", "device-a", "active", now)))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("b", "device-b", "active", now)))

	require.NoError(t, repo.DeleteDeviceData("device-a"))
	ids, err := repo.ListScheduledSendIDs()
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, ids)

	require.NoError(t, repo.TruncateAllChats())
	ids, err = repo.ListScheduledSendIDs()
	require.NoError(t, err)
	require.Empty(t, ids)
}

func TestSQLiteRepositoryListScheduledSendsOrdersUpcomingThenHistory(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("active", "device-a", "active", now.Add(2*time.Minute))))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("paused", "device-a", "paused", now.Add(time.Minute))))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("running", "device-a", "running", now.Add(-time.Minute))))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("completed-old", "device-a", "completed", now.Add(-time.Hour))))
	require.NoError(t, repo.CreateScheduledSend(newScheduledSendFixture("failed-new", "device-a", "failed", now.Add(-2*time.Hour))))
	_, err := repo.db.Exec(`UPDATE scheduled_sends SET updated_at = ? WHERE id = ?`, now.Add(-time.Hour), "completed-old")
	require.NoError(t, err)
	_, err = repo.db.Exec(`UPDATE scheduled_sends SET updated_at = ? WHERE id = ?`, now.Add(-time.Minute), "failed-new")
	require.NoError(t, err)

	jobs, err := repo.ListScheduledSends(domainChatStorage.ScheduledSendFilter{DeviceID: "device-a"})
	require.NoError(t, err)
	ids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	require.Equal(t, []string{"running", "paused", "active", "failed-new", "completed-old"}, ids)
}

func timePointer(value time.Time) *time.Time {
	return &value
}
