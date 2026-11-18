package usecase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

func TestScheduleStaleFailureKeepsCurrentWorkerAssets(t *testing.T) {
	for _, dispatchErr := range []error{pkgError.ValidationError("late validation error"), errScheduledPayload} {
		t.Run(dispatchErr.Error(), func(t *testing.T) {
			base := &scheduleIDSendStub{}
			service, repo := newScheduleTestService(t, base)
			job := claimScheduleTestJob(t, service, repo, newScheduleTestJob("stale-media", "once", scheduleTestNow.Add(-time.Minute)))
			media := filepath.Join(service.mediaRoot, "scheduled", job.ID, "image.png")
			require.NoError(t, os.WriteFile(media, []byte("must survive takeover"), 0600))
			base.fail = func(int) error {
				service.now = func() time.Time { return job.LeaseUntil.Add(time.Second) }
				service.recoverInterrupted()
				current, err := repo.ClaimNextScheduledSend(service.now(), service.now().Add(2*time.Minute), "current-worker")
				require.NoError(t, err)
				require.NotNil(t, current)
				return dispatchErr
			}
			require.ErrorIs(t, service.deliver(scheduleTestContext(), job), dispatchErr)
			stored, err := repo.GetScheduledSend("device-a", job.ID)
			require.NoError(t, err)
			require.Equal(t, "current-worker", stored.LeaseToken)
			require.Equal(t, scheduleStatusRunning, stored.Status)
			require.Equal(t, job.PendingMessageID, stored.PendingMessageID)
			require.NotEmpty(t, stored.PendingMessageID)
			require.Equal(t, 1, stored.Attempts)
			require.FileExists(t, media)
		})
	}
}

func TestScheduleStaleSkipKeepsPendingAssets(t *testing.T) {
	for _, status := range []string{scheduleStatusActive, scheduleStatusRunning, scheduleStatusPaused} {
		t.Run(status, func(t *testing.T) {
			service, repo := newScheduleTestService(t, &scheduleIDSendStub{})
			job := claimScheduleTestJob(t, service, repo, newScheduleTestJob("stale-skip", "once", scheduleTestNow))
			_, err := service.pinOccurrenceMessageID(context.Background(), job)
			require.NoError(t, err)
			service.now = func() time.Time { return job.LeaseUntil.Add(time.Second) }
			service.recoverInterrupted()
			if status == scheduleStatusRunning {
				_, err = repo.ClaimNextScheduledSend(service.now(), service.now().Add(time.Minute), "current-worker")
				require.NoError(t, err)
			} else if status == scheduleStatusPaused {
				require.NoError(t, service.Pause(scheduleTestContext(), job.ID))
			}
			before, err := repo.GetScheduledSend(job.DeviceID, job.ID)
			require.NoError(t, err)
			require.NoError(t, service.skipOccurrence(job, "late exhausted or legacy attempt"))
			after, err := repo.GetScheduledSend(job.DeviceID, job.ID)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.DirExists(t, filepath.Join(service.mediaRoot, "scheduled", job.ID))
		})
	}
}

type scheduleCleanupErrorRepo struct {
	domainChatStorage.IChatStorageRepository
	failErr error
	readErr error
}

func (r *scheduleCleanupErrorRepo) FailScheduledSend(id, token, reason string) error {
	if r.failErr != nil {
		return r.failErr
	}
	return r.IChatStorageRepository.FailScheduledSend(id, token, reason)
}

func (r *scheduleCleanupErrorRepo) GetScheduledSend(deviceID, id string) (*domainChatStorage.ScheduledSend, error) {
	if r.readErr != nil {
		return nil, r.readErr
	}
	return r.IChatStorageRepository.GetScheduledSend(deviceID, id)
}

func TestScheduleTerminalCleanupKeepsAssetsOnDatabaseError(t *testing.T) {
	for _, path := range []string{"deliver", "skip"} {
		for _, operation := range []string{"write", "read"} {
			t.Run(path+"/"+operation, func(t *testing.T) {
				base := &scheduleIDSendStub{fail: func(int) error { return pkgError.ValidationError("invalid payload") }}
				service, repo := newScheduleTestService(t, base)
				job := claimScheduleTestJob(t, service, repo, newScheduleTestJob("db-error", "once", scheduleTestNow))
				dbErr := errors.New("database unavailable")
				wrapper := &scheduleCleanupErrorRepo{IChatStorageRepository: repo}
				if operation == "write" {
					wrapper.failErr = dbErr
				} else {
					wrapper.readErr = dbErr
				}
				service.repo = wrapper
				var err error
				if path == "deliver" {
					err = service.deliver(scheduleTestContext(), job)
				} else {
					err = service.skipOccurrence(job, "exhausted")
				}
				if operation == "write" {
					require.ErrorIs(t, err, dbErr)
				}
				require.DirExists(t, filepath.Join(service.mediaRoot, "scheduled", job.ID))
			})
		}
	}
}

func TestScheduleTerminalStatesCannotResume(t *testing.T) {
	for _, status := range []string{scheduleStatusCompleted, scheduleStatusFailed, scheduleStatusCancelled} {
		t.Run(status, func(t *testing.T) {
			service, repo := newScheduleTestService(t, &scheduleIDSendStub{})
			job := newScheduleTestJob("terminal", "once", scheduleTestNow)
			job.Status = status
			require.NoError(t, repo.CreateScheduledSend(job))
			require.Error(t, service.Resume(scheduleTestContext(), job.ID))
			stored, err := repo.GetScheduledSend(job.DeviceID, job.ID)
			require.NoError(t, err)
			require.Equal(t, status, stored.Status)
		})
	}
}
