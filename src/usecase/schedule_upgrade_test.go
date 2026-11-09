package usecase

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Upgrade the actual v49 schema, including a send that may already have been
// delivered by the old worker. Missing pinned IDs must not authorize replay.
func TestScheduleUpgradeDoesNotReplayLegacyUnknownAttempts(t *testing.T) {
	for _, recurrence := range []string{"once", "daily"} {
		for _, state := range []string{"running", "active", "paused", "unattempted"} {
			t.Run(recurrence+"/"+state, func(t *testing.T) {
				db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "legacy.db"))
				require.NoError(t, err)
				defer db.Close()
				schema, err := os.ReadFile("testdata/chatstorage_v49.sql")
				require.NoError(t, err)
				_, err = db.Exec(string(schema))
				require.NoError(t, err)
				slot := scheduleTestNow.Add(-time.Minute)
				status, attempts := state, 1
				if state == "running" {
					attempts = 0
				} // old worker hadn't recorded its result
				if state == "unattempted" {
					status, attempts = "active", 0
				}
				_, err = db.Exec(`INSERT INTO scheduled_sends
     (id, device_id, message_type, payload_json, scheduled_at, next_run_at,
      timezone, recurrence, status, attempts, lease_token, lease_until)
     VALUES ('job', 'device-a', 'text', '{"phone":"628123456789","message":"hello"}',
      ?, ?, 'UTC', ?, ?, ?, 'old-worker', ?)`, slot, slot, recurrence, status, attempts, slot)
				require.NoError(t, err)
				repo := chatstorage.NewStorageRepository(db)
				require.NoError(t, repo.InitializeSchema())
				require.NoError(t, repo.InitializeSchema(), "upgrade must be repeatable")
				base := &scheduleIDSendStub{}
				service := NewScheduleService(repo, base, nil, t.TempDir())
				service.now = func() time.Time { return scheduleTestNow }
				stored, err := repo.GetScheduledSend("device-a", "job")
				require.NoError(t, err)
				require.Equal(t, state != "unattempted", stored.LegacyAttemptUnknown)
				service.recoverInterrupted()
				if state == "paused" {
					require.NoError(t, service.Resume(scheduleTestContext(), "job"))
				}
				// Claim only jobs due now; never mistake the next recurring slot for a replay.
				claimed, err := repo.ClaimNextScheduledSend(scheduleTestNow, scheduleTestNow.Add(time.Minute), "new-worker")
				require.NoError(t, err)
				if claimed != nil {
					require.NoError(t, service.deliver(scheduleTestContext(), claimed))
				}
				stored, err = repo.GetScheduledSend("device-a", "job")
				require.NoError(t, err)
				if state == "unattempted" {
					require.Len(t, base.ids, 1, "unattempted legacy jobs remain eligible")
				} else {
					require.Empty(t, base.ids, "unknown legacy occurrences must never be replayed")
					if recurrence == "once" {
						require.Equal(t, scheduleStatusFailed, stored.Status)
					} else {
						require.Equal(t, scheduleStatusActive, stored.Status)
						require.True(t, stored.NextRunAt.After(scheduleTestNow))
						require.False(t, stored.LegacyAttemptUnknown)
						// The next slot is a safely new occurrence.
						require.NoError(t, service.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, repo, "next-slot")))
						require.Len(t, base.ids, 1)
					}
				}
			})
		}
	}
}
