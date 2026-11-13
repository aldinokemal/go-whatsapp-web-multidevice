package usecase

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// newScheduleTestServiceAt builds a scheduler with its own connection to the
// database at path, as a restarted process or a second worker has.
func newScheduleTestServiceAt(t *testing.T, path, mediaRoot string, base domainSend.ISendUsecase, now time.Time) (*ScheduleService, domainChatStorage.IChatStorageRepository) {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())
	service := NewScheduleService(repo, base, nil, mediaRoot)
	service.now = func() time.Time { return now }
	return service, repo
}

// TestScheduledCrashMidSendResumesTheOccurrenceUnderItsMessageID: the process
// stops after the occurrence's id was pinned, with the send possibly on its
// way. The restarted scheduler must send that same occurrence under that same
// id and secret, keeping a recurring schedule on the interrupted slot.
func TestScheduledCrashMidSendResumesTheOccurrenceUnderItsMessageID(t *testing.T) {
	for _, recurrence := range []string{"once", "daily"} {
		t.Run(recurrence, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "chatstorage.db")
			mediaRoot := t.TempDir()
			slot := scheduleTestNow.Add(-time.Minute)

			crashed, crashedRepo := newScheduleTestServiceAt(t, path, mediaRoot, &scheduleIDSendStub{}, scheduleTestNow)
			claimed := claimScheduleTestJob(t, crashed, crashedRepo, newScheduleTestJob("job", recurrence, slot))
			pinnedCtx, err := crashed.pinOccurrenceMessageID(scheduleTestContext(), claimed)
			require.NoError(t, err)
			pinned := whatsapp.SendMessageIDFromContext(pinnedCtx)
			require.NotEmpty(t, pinned)
			// The process dies here: nothing records how the send ended.

			base := &scheduleIDSendStub{}
			afterLease := scheduleTestNow.Add(3 * time.Minute)
			restarted, repo := newScheduleTestServiceAt(t, path, mediaRoot, base, afterLease)
			restarted.recoverInterrupted()

			stored, err := repo.GetScheduledSend("device-a", "job")
			require.NoError(t, err)
			require.Equal(t, scheduleStatusActive, stored.Status, "an interrupted occurrence must be queued again")
			require.Equal(t, pinned, stored.PendingMessageID)
			require.True(t, slot.Equal(stored.NextRunAt), "the schedule must stay on the interrupted slot, got %s", stored.NextRunAt)
			require.Zero(t, stored.OccurrenceCount)
			require.Equal(t, 1, stored.Attempts)

			require.NoError(t, restarted.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, repo, "lease-2")))
			require.Equal(t, []string{pinned}, base.ids, "the resumed send must use the pinned id")

			stored, err = repo.GetScheduledSend("device-a", "job")
			require.NoError(t, err)
			require.Equal(t, 1, stored.OccurrenceCount)
			require.Equal(t, pinned, stored.LastMessageID)
			require.Empty(t, stored.PendingMessageID, "the id is released once the send is recorded")
			if recurrence == "once" {
				require.Equal(t, scheduleStatusCompleted, stored.Status)
				return
			}
			require.Equal(t, scheduleStatusActive, stored.Status)
			require.True(t, slot.Add(24*time.Hour).Equal(stored.NextRunAt), "next run %s", stored.NextRunAt)

			// The following occurrence is a new message.
			require.NoError(t, restarted.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, repo, "lease-3")))
			require.Len(t, base.ids, 2)
			require.NotEqual(t, pinned, base.ids[1])
		})
	}
}

// TestScheduledLeaseExpiryMidSendResendsUnderTheSameMessageID: a send outlives
// its lease and a second worker takes the occurrence over. Both send it, under
// the one pinned id; only the worker holding the lease records the result, and
// the first must not remove media the second still needs.
func TestScheduledLeaseExpiryMidSendResendsUnderTheSameMessageID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chatstorage.db")
	mediaRoot := t.TempDir()
	assets := filepath.Join(mediaRoot, "scheduled", "job")

	secondBase := &scheduleIDSendStub{}
	second, secondRepo := newScheduleTestServiceAt(t, path, mediaRoot, secondBase, scheduleTestNow.Add(3*time.Minute))

	var taken *domainChatStorage.ScheduledSend
	firstBase := &scheduleIDSendStub{}
	firstBase.fail = func(int) error {
		// The first worker's send is in flight when its lease lapses.
		second.processDue(context.Background())
		stored, err := secondRepo.GetScheduledSend("device-a", "job")
		require.NoError(t, err)
		require.Equal(t, scheduleStatusActive, stored.Status, "an occurrence whose lease lapsed must be queued again")
		require.Equal(t, firstBase.ids[0], stored.PendingMessageID)
		taken = reclaimScheduleTestJob(t, secondRepo, "lease-2")
		return nil
	}
	first, firstRepo := newScheduleTestServiceAt(t, path, mediaRoot, firstBase, scheduleTestNow)
	claimed := claimScheduleTestJob(t, first, firstRepo, newScheduleTestJob("job", "once", scheduleTestNow.Add(-time.Minute)))

	require.NoError(t, first.deliver(scheduleTestContext(), claimed))

	stored, err := firstRepo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusRunning, stored.Status, "a worker that lost its lease must not record the result")
	require.Equal(t, "lease-2", stored.LeaseToken)
	require.Zero(t, stored.OccurrenceCount)
	_, statErr := os.Stat(assets)
	require.NoError(t, statErr, "the worker that took over still needs the media")

	require.NotNil(t, taken)
	require.NoError(t, second.deliver(scheduleTestContext(), taken))
	require.Len(t, firstBase.ids, 1)
	require.Equal(t, firstBase.ids, secondBase.ids, "both workers must send under the same id")

	stored, err = secondRepo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusCompleted, stored.Status)
	require.Equal(t, 1, stored.OccurrenceCount)
	require.Equal(t, firstBase.ids[0], stored.LastMessageID)
	require.Empty(t, stored.PendingMessageID)
	_, statErr = os.Stat(assets)
	require.True(t, os.IsNotExist(statErr))
}

// TestScheduledInterruptedOccurrenceBeforeItsSendCostsNoAttempt: with no id
// pinned the send was never reached, so the occurrence is simply queued again.
func TestScheduledInterruptedOccurrenceBeforeItsSendCostsNoAttempt(t *testing.T) {
	service, repo := newScheduleTestService(t, &scheduleIDSendStub{})
	slot := scheduleTestNow.Add(-time.Hour)
	job := newScheduleTestJob("job", "daily", slot)
	job.Status = scheduleStatusRunning
	job.LeaseToken = "stale"
	job.Attempts = 2
	leaseUntil := scheduleTestNow.Add(-time.Minute)
	job.LeaseUntil = &leaseUntil
	require.NoError(t, repo.CreateScheduledSend(job))

	service.recoverInterrupted()

	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusActive, stored.Status)
	require.Equal(t, 2, stored.Attempts)
	require.True(t, slot.Equal(stored.NextRunAt))
}

// TestScheduledInterruptedOccurrenceIsGivenUpWhenItsBudgetIsSpent: a send that
// keeps interrupting the worker is charged like one that keeps failing, and
// the occurrence after it starts with a fresh id.
func TestScheduledInterruptedOccurrenceIsGivenUpWhenItsBudgetIsSpent(t *testing.T) {
	service, repo := newScheduleTestService(t, &scheduleIDSendStub{})
	for _, recurrence := range []string{"once", "daily"} {
		job := newScheduleTestJob(recurrence, recurrence, scheduleTestNow.Add(-time.Hour))
		job.Status = scheduleStatusRunning
		job.LeaseToken = "stale"
		job.Attempts = scheduleMaxAttempts - 1
		job.PendingMessageID = "3EB0INTERRUPTED"
		leaseUntil := scheduleTestNow.Add(-time.Minute)
		job.LeaseUntil = &leaseUntil
		require.NoError(t, repo.CreateScheduledSend(job))
	}

	service.recoverInterrupted()

	once, err := repo.GetScheduledSend("device-a", "once")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusFailed, once.Status)

	daily, err := repo.GetScheduledSend("device-a", "daily")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusActive, daily.Status)
	require.True(t, scheduleTestNow.Add(23*time.Hour).Equal(daily.NextRunAt))
	require.Zero(t, daily.Attempts)
	require.Empty(t, daily.PendingMessageID)
}

// TestScheduleResumeAtALaterSlotDropsTheMessageID: a paused recurring job that
// resumes at its next slot gives up the missed occurrence, whose id may have
// reached WhatsApp; the next occurrence must not be sent as its duplicate. A
// one-time job resumes the same occurrence and keeps the id.
func TestScheduleResumeAtALaterSlotDropsTheMessageID(t *testing.T) {
	for recurrence, want := range map[string]string{"once": "3EB0PAUSED", "daily": ""} {
		t.Run(recurrence, func(t *testing.T) {
			service, repo := newScheduleTestService(t, &scheduleIDSendStub{})
			job := newScheduleTestJob("job", recurrence, scheduleTestNow.Add(-time.Hour))
			job.Status = scheduleStatusPaused
			job.Attempts = 1
			job.PendingMessageID = "3EB0PAUSED"
			job.PendingPollSecret = make([]byte, 32)
			require.NoError(t, repo.CreateScheduledSend(job))

			require.NoError(t, service.Resume(scheduleTestContext(), "job"))

			stored, err := repo.GetScheduledSend("device-a", "job")
			require.NoError(t, err)
			require.Equal(t, scheduleStatusActive, stored.Status)
			require.Equal(t, want, stored.PendingMessageID)
			if recurrence == "once" {
				require.Len(t, stored.PendingPollSecret, 32)
			} else {
				require.Empty(t, stored.PendingPollSecret)
			}
		})
	}
}
