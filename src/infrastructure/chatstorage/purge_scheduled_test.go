package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
)

// Retention also purges scheduled sends that are finished (completed, failed
// or cancelled) and were last touched before the cutoff. Anything that may
// still send (active, running, paused) is kept, however old.

// storeScheduled inserts a job, then pins updated_at (Create stamps it with
// the wall clock).
func storeScheduled(t *testing.T, repo *SQLiteRepository, id, deviceID, status string, updated time.Time) {
	t.Helper()
	at := purgeCutoff.Add(-30 * 24 * time.Hour)
	require.NoError(t, repo.CreateScheduledSend(&domainChatStorage.ScheduledSend{
		ID: id, DeviceID: deviceID, MessageType: "text", PayloadJSON: "{}", AssetsJSON: "{}",
		ScheduledAt: at, NextRunAt: at, Timezone: "UTC", Recurrence: "once", WeekdaysJSON: "[]",
		Status: status, CreatedAt: at,
	}))
	_, err := repo.db.Exec(`UPDATE scheduled_sends SET updated_at = ? WHERE id = ?`, updated, id)
	require.NoError(t, err)
}

func scheduledExists(t *testing.T, repo *SQLiteRepository, id string) bool {
	t.Helper()
	var n int
	require.NoError(t, repo.db.QueryRow(`SELECT COUNT(*) FROM scheduled_sends WHERE id = ?`, id).Scan(&n))
	return n == 1
}

// Behaviour: finished jobs strictly older than the cutoff go, on every device;
// finished jobs at or after it stay.
func TestPurgeFinishedScheduledSendsBoundary(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	plus14 := time.FixedZone("plus14", 14*3600)
	minus5 := time.FixedZone("minus5", -5*3600)
	for _, status := range []string{"completed", "failed", "cancelled"} {
		storeScheduled(t, repo, status+"-before-a", devA, status, purgeCutoff.Add(-time.Second))
		storeScheduled(t, repo, status+"-before-b", devB, status, purgeCutoff.Add(-time.Second))
		storeScheduled(t, repo, status+"-before-plus14", devA, status, purgeCutoff.Add(-time.Second).In(plus14))
		storeScheduled(t, repo, status+"-at", devA, status, purgeCutoff)
		storeScheduled(t, repo, status+"-at-minus5", devA, status, purgeCutoff.In(minus5))
		storeScheduled(t, repo, status+"-after", devB, status, purgeCutoff.Add(time.Second))
	}

	n, err := repo.PurgeFinishedScheduledSendsBefore(purgeCutoff)
	require.NoError(t, err)
	require.EqualValues(t, 9, n)
	for _, status := range []string{"completed", "failed", "cancelled"} {
		require.False(t, scheduledExists(t, repo, status+"-before-a"), status)
		require.False(t, scheduledExists(t, repo, status+"-before-b"), status)
		require.False(t, scheduledExists(t, repo, status+"-before-plus14"), status)
		require.True(t, scheduledExists(t, repo, status+"-at"), status)
		require.True(t, scheduledExists(t, repo, status+"-at-minus5"), status)
		require.True(t, scheduledExists(t, repo, status+"-after"), status)
	}
}

// Behaviour: a job that may still send is never purged, however old.
func TestPurgeKeepsPendingScheduledSends(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	ancient := purgeCutoff.Add(-400 * 24 * time.Hour)
	for _, status := range []string{"active", "running", "paused"} {
		storeScheduled(t, repo, status, devA, status, ancient)
	}
	n, err := repo.PurgeFinishedScheduledSendsBefore(purgeCutoff)
	require.NoError(t, err)
	require.Zero(t, n)
	for _, status := range []string{"active", "running", "paused"} {
		require.True(t, scheduledExists(t, repo, status), status)
	}
}

// Behaviour: a zero cutoff is refused and deletes nothing.
func TestPurgeScheduledSendsNeedsACutoff(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	storeScheduled(t, repo, "old", devA, "completed", purgeCutoff.Add(-400*24*time.Hour))
	_, err := repo.PurgeFinishedScheduledSendsBefore(time.Time{})
	require.Error(t, err)
	require.True(t, scheduledExists(t, repo, "old"))
}

// Behaviour: the scheduled purge is its own step; it never touches messages.
func TestPurgeScheduledSendsLeavesMessagesAlone(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	storeChat(t, repo, devA, chat1, purgeCutoff.Add(-time.Hour))
	storeMsg(t, repo, devA, chat1, "old", purgeCutoff.Add(-time.Hour))
	storeScheduled(t, repo, "done", devA, "completed", purgeCutoff.Add(-time.Hour))
	_, err := repo.PurgeFinishedScheduledSendsBefore(purgeCutoff)
	require.NoError(t, err)
	require.True(t, messageExists(t, repo, devA, chat1, "old"))
	require.False(t, scheduledExists(t, repo, "done"))
}

// Mechanism: a large backlog is purged in several bounded transactions.
func TestPurgeScheduledSendsRunsInBatches(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	for i := 0; i < 7; i++ {
		storeScheduled(t, repo, "job-"+string(rune('a'+i)), devA, "completed", purgeCutoff.Add(-time.Hour))
	}
	commits := 0
	defer setPurgeBatchForTest(3, func() { commits++ })()
	n, err := repo.PurgeFinishedScheduledSendsBefore(purgeCutoff)
	require.NoError(t, err)
	require.EqualValues(t, 7, n)
	require.Equal(t, 3, commits)
}
