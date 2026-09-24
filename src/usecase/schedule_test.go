package usecase

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
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
	sendText func() (domainSend.GenericResponse, error)
}

func (s *scheduleSendStub) SendText(context.Context, domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	return s.sendText()
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

func TestHydrateAssetStreamsLargeMediaIntact(t *testing.T) {
	// Larger than ReadForm's memory limit, so the part goes through a temp file.
	data := bytes.Repeat([]byte("0123456789abcdef"), 1<<17)
	path := filepath.Join(t.TempDir(), "clip.mp4")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	hydrated, err := hydrateAsset(scheduledAsset{Path: path, Filename: "clip.mp4", ContentType: "video/mp4"}, "video")
	require.NoError(t, err)
	defer hydrated.cleanup()

	file, err := hydrated.Header.Open()
	require.NoError(t, err)
	defer file.Close()
	got, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, data, got)
}

var scheduleTestNow = time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)

func newScheduleTestService(t *testing.T, base domainSend.ISendUsecase) (*ScheduleService, domainChatStorage.IChatStorageRepository) {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chatstorage.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())
	service := NewScheduleService(repo, base, nil, t.TempDir())
	service.now = func() time.Time { return scheduleTestNow }
	return service, repo
}

func newScheduleTestJob(id, recurrence string, nextRunAt time.Time) *domainChatStorage.ScheduledSend {
	return &domainChatStorage.ScheduledSend{
		ID: id, DeviceID: "device-a", MessageType: "text", PayloadJSON: `{"phone":"628123456789","message":"hello"}`,
		AssetsJSON: "{}", Phone: "628123456789", ScheduledAt: nextRunAt, NextRunAt: nextRunAt,
		Timezone: "UTC", Recurrence: recurrence, WeekdaysJSON: "[]", Status: scheduleStatusActive,
	}
}

// claimScheduleTestJob stores job and leases it the way the worker does.
func claimScheduleTestJob(t *testing.T, service *ScheduleService, repo domainChatStorage.IChatStorageRepository, job *domainChatStorage.ScheduledSend) *domainChatStorage.ScheduledSend {
	t.Helper()
	require.NoError(t, repo.CreateScheduledSend(job))
	require.NoError(t, os.MkdirAll(filepath.Join(service.mediaRoot, "scheduled", job.ID), 0o700))
	claimed, err := repo.ClaimNextScheduledSend(scheduleTestNow, scheduleTestNow.Add(2*time.Minute), "lease-"+job.ID)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	return claimed
}

func scheduleTestContext() context.Context {
	return whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("device-a", nil, nil))
}

func TestScheduleDeliverClassifiesFailures(t *testing.T) {
	tests := []struct {
		name         string
		recurrence   string
		attempts     int
		send         func() (domainSend.GenericResponse, error)
		wantStatus   string
		wantAttempts int
		wantNext     time.Time
		wantAssets   bool
	}{
		{
			name:       "transient error retries and counts the attempt",
			recurrence: "once",
			send: func() (domainSend.GenericResponse, error) {
				return domainSend.GenericResponse{}, errors.New("info query timed out")
			},
			wantStatus:   scheduleStatusActive,
			wantAttempts: 1,
			wantNext:     scheduleTestNow.Add(15 * time.Second),
			wantAssets:   true,
		},
		{
			name:       "invalid jid is transient",
			recurrence: "once",
			attempts:   2,
			send: func() (domainSend.GenericResponse, error) {
				return domainSend.GenericResponse{}, pkgError.InvalidJID("user is not registered")
			},
			wantStatus:   scheduleStatusActive,
			wantAttempts: 3,
			wantNext:     scheduleTestNow.Add(time.Minute),
			wantAssets:   true,
		},
		{
			name:         "panic is recovered into a retry",
			recurrence:   "once",
			send:         func() (domainSend.GenericResponse, error) { panic(pkgError.ErrNotConnected) },
			wantStatus:   scheduleStatusActive,
			wantAttempts: 1,
			wantNext:     scheduleTestNow.Add(15 * time.Second),
			wantAssets:   true,
		},
		{
			name:       "validation error fails permanently",
			recurrence: "once",
			send: func() (domainSend.GenericResponse, error) {
				return domainSend.GenericResponse{}, pkgError.ValidationError("phone is invalid")
			},
			wantStatus: scheduleStatusFailed,
		},
		{
			name:       "exhausted one-time job fails",
			recurrence: "once",
			attempts:   scheduleMaxAttempts - 1,
			send: func() (domainSend.GenericResponse, error) {
				return domainSend.GenericResponse{}, errors.New("upload failed")
			},
			wantStatus:   scheduleStatusFailed,
			wantAttempts: scheduleMaxAttempts - 1,
		},
		{
			name:       "exhausted recurring job skips to its next slot",
			recurrence: "daily",
			attempts:   scheduleMaxAttempts - 1,
			send: func() (domainSend.GenericResponse, error) {
				return domainSend.GenericResponse{}, errors.New("upload failed")
			},
			wantStatus: scheduleStatusActive,
			wantNext:   scheduleTestNow.Add(24*time.Hour - time.Minute),
			wantAssets: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo := newScheduleTestService(t, &scheduleSendStub{sendText: tt.send})
			job := newScheduleTestJob("job", tt.recurrence, scheduleTestNow.Add(-time.Minute))
			job.Attempts = tt.attempts
			claimed := claimScheduleTestJob(t, service, repo, job)

			require.Error(t, service.deliver(context.Background(), claimed))

			stored, err := repo.GetScheduledSend("device-a", "job")
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, stored.Status)
			require.Equal(t, tt.wantAttempts, stored.Attempts)
			require.NotEmpty(t, stored.LastError)
			if !tt.wantNext.IsZero() {
				require.True(t, tt.wantNext.Equal(stored.NextRunAt), "next run %s, want %s", stored.NextRunAt, tt.wantNext)
			}
			_, statErr := os.Stat(filepath.Join(service.mediaRoot, "scheduled", "job"))
			require.Equal(t, tt.wantAssets, statErr == nil)
		})
	}
}

func TestScheduleDeliverFailsForwardOfDeletedMessage(t *testing.T) {
	service, repo := newScheduleTestService(t, &scheduleSendStub{})
	job := newScheduleTestJob("job", "daily", scheduleTestNow.Add(-time.Minute))
	job.MessageType = "forward"
	job.PayloadJSON = `{"phone":"628123456789","message_id":"gone"}`
	claimed := claimScheduleTestJob(t, service, repo, job)

	require.Error(t, service.deliver(scheduleTestContext(), claimed))

	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusFailed, stored.Status)
	require.Contains(t, stored.LastError, "not found")
}

func TestScheduleProcessJobOfflineDeviceKeepsAttempts(t *testing.T) {
	manager := whatsapp.NewDeviceManager(nil, nil, nil)
	manager.AddDevice(whatsapp.NewDeviceInstance("device-a", nil, nil))
	for name, dm := range map[string]*whatsapp.DeviceManager{"missing manager": nil, "device without client": manager} {
		t.Run(name, func(t *testing.T) {
			service, repo := newScheduleTestService(t, &scheduleSendStub{sendText: func() (domainSend.GenericResponse, error) {
				t.Fatal("offline device must not send")
				return domainSend.GenericResponse{}, nil
			}})
			service.manager = dm
			job := newScheduleTestJob("job", "once", scheduleTestNow.Add(-time.Minute))
			job.Attempts = 4
			claimed := claimScheduleTestJob(t, service, repo, job)

			require.NoError(t, service.processJob(context.Background(), claimed))

			stored, err := repo.GetScheduledSend("device-a", "job")
			require.NoError(t, err)
			require.Equal(t, scheduleStatusActive, stored.Status)
			require.Equal(t, 4, stored.Attempts)
			require.True(t, scheduleTestNow.Add(30*time.Second).Equal(stored.NextRunAt))
		})
	}
}

func TestScheduleProcessJobEndsOverdueSeriesPastEndAt(t *testing.T) {
	service, repo := newScheduleTestService(t, &scheduleSendStub{sendText: func() (domainSend.GenericResponse, error) {
		t.Fatal("no send may happen after end_at")
		return domainSend.GenericResponse{}, nil
	}})
	// The occurrence was due before end_at but is only picked up after it.
	job := newScheduleTestJob("job", "daily", scheduleTestNow.Add(-2*time.Hour))
	endAt := scheduleTestNow.Add(-time.Hour)
	job.EndAt = &endAt
	claimed := claimScheduleTestJob(t, service, repo, job)

	require.NoError(t, service.processJob(context.Background(), claimed))

	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusCompleted, stored.Status)
	_, statErr := os.Stat(filepath.Join(service.mediaRoot, "scheduled", "job"))
	require.True(t, os.IsNotExist(statErr))
}

func TestScheduleProcessDueRecoversInterruptedJobsWithoutResending(t *testing.T) {
	service, repo := newScheduleTestService(t, &scheduleSendStub{sendText: func() (domainSend.GenericResponse, error) {
		t.Fatal("an interrupted occurrence must not be resent")
		return domainSend.GenericResponse{}, nil
	}})
	for _, job := range []*domainChatStorage.ScheduledSend{
		newScheduleTestJob("once", "once", scheduleTestNow.Add(-time.Hour)),
		newScheduleTestJob("daily", "daily", scheduleTestNow.Add(-time.Hour)),
	} {
		job.Status = scheduleStatusRunning
		job.LeaseToken = "stale"
		leaseUntil := scheduleTestNow.Add(-time.Minute)
		job.LeaseUntil = &leaseUntil
		require.NoError(t, repo.CreateScheduledSend(job))
		require.NoError(t, os.MkdirAll(filepath.Join(service.mediaRoot, "scheduled", job.ID), 0o700))
	}

	service.processDue(context.Background())

	once, err := repo.GetScheduledSend("device-a", "once")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusFailed, once.Status)
	_, statErr := os.Stat(filepath.Join(service.mediaRoot, "scheduled", "once"))
	require.True(t, os.IsNotExist(statErr))

	daily, err := repo.GetScheduledSend("device-a", "daily")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusActive, daily.Status)
	require.True(t, scheduleTestNow.Add(23*time.Hour).Equal(daily.NextRunAt))
	_, statErr = os.Stat(filepath.Join(service.mediaRoot, "scheduled", "daily"))
	require.NoError(t, statErr)
}

func TestScheduleResumeOverdueJobs(t *testing.T) {
	endAt := scheduleTestNow.Add(-time.Minute)
	tests := []struct {
		name       string
		recurrence string
		endAt      *time.Time
		wantStatus string
		wantNext   time.Time
		wantAssets bool
	}{
		{name: "one-time job sends now", recurrence: "once", wantStatus: scheduleStatusActive, wantNext: scheduleTestNow, wantAssets: true},
		{name: "recurring job jumps to its next slot", recurrence: "daily", wantStatus: scheduleStatusActive, wantNext: scheduleTestNow.Add(23 * time.Hour), wantAssets: true},
		{name: "recurring job past end_at completes", recurrence: "daily", endAt: &endAt, wantStatus: scheduleStatusCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo := newScheduleTestService(t, &scheduleSendStub{})
			job := newScheduleTestJob("job", tt.recurrence, scheduleTestNow.Add(-time.Hour))
			job.Status = scheduleStatusPaused
			job.EndAt = tt.endAt
			require.NoError(t, repo.CreateScheduledSend(job))
			require.NoError(t, os.MkdirAll(filepath.Join(service.mediaRoot, "scheduled", "job"), 0o700))

			require.NoError(t, service.Resume(scheduleTestContext(), "job"))

			stored, err := repo.GetScheduledSend("device-a", "job")
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, stored.Status)
			if !tt.wantNext.IsZero() {
				require.True(t, tt.wantNext.Equal(stored.NextRunAt), "next run %s, want %s", stored.NextRunAt, tt.wantNext)
			}
			_, statErr := os.Stat(filepath.Join(service.mediaRoot, "scheduled", "job"))
			require.Equal(t, tt.wantAssets, statErr == nil)
		})
	}
}

func TestScheduleStatusChangesReportNotFoundAndConflicts(t *testing.T) {
	service, repo := newScheduleTestService(t, &scheduleSendStub{})
	ctx := scheduleTestContext()
	running := newScheduleTestJob("running", "once", scheduleTestNow)
	running.Status = scheduleStatusRunning
	require.NoError(t, repo.CreateScheduledSend(running))

	_, err := service.Get(ctx, "missing")
	require.ErrorIs(t, err, pkgError.ErrScheduledSendNotFound)
	require.ErrorIs(t, service.Cancel(ctx, "missing"), pkgError.ErrScheduledSendNotFound)

	var validationErr pkgError.ValidationError
	require.ErrorAs(t, service.Pause(ctx, "running"), &validationErr)
	require.ErrorContains(t, service.Pause(ctx, "running"), "cannot be paused while running")
	require.ErrorAs(t, service.Cancel(ctx, "running"), &validationErr)
	require.ErrorAs(t, service.Resume(ctx, "running"), &validationErr)
}

type scheduleIDsRepoStub struct {
	domainChatStorage.IChatStorageRepository
	ids []string
	err error
}

func (r *scheduleIDsRepoStub) ListScheduledSendIDs() ([]string, error) {
	return r.ids, r.err
}

func TestScheduleGarbageCollectAssets(t *testing.T) {
	tests := []struct {
		name     string
		repo     *scheduleIDsRepoStub
		wantLive bool
		wantDead bool
	}{
		{name: "removes directories without a live job", repo: &scheduleIDsRepoStub{ids: []string{"live"}}, wantLive: true},
		{name: "deletes nothing when the live set is unknown", repo: &scheduleIDsRepoStub{err: errors.New("database is locked")}, wantLive: true, wantDead: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewScheduleService(tt.repo, &scheduleSendStub{}, nil, t.TempDir())
			for _, id := range []string{"live", "dead"} {
				require.NoError(t, os.MkdirAll(filepath.Join(service.mediaRoot, "scheduled", id), 0o700))
			}

			service.garbageCollectAssets()

			_, liveErr := os.Stat(filepath.Join(service.mediaRoot, "scheduled", "live"))
			_, deadErr := os.Stat(filepath.Join(service.mediaRoot, "scheduled", "dead"))
			require.Equal(t, tt.wantLive, liveErr == nil)
			require.Equal(t, tt.wantDead, deadErr == nil)
		})
	}
}

func TestScheduledSendRejectsUnschedulableRequests(t *testing.T) {
	service, _ := newScheduleTestService(t, &scheduleSendStub{})
	decorated := NewScheduledSendService(&scheduleSendStub{}, service)
	options := domainSend.ScheduleOptions{ScheduledAt: scheduleTestNow.Add(time.Hour).Format(time.RFC3339), Timezone: "UTC"}
	var validationErr pkgError.ValidationError

	_, err := decorated.SendChatPresence(scheduleTestContext(), domainSend.ChatPresenceRequest{
		BaseRequest: domainSend.BaseRequest{ScheduleOptions: options}, Phone: "628123456789", Action: "start",
	})
	require.ErrorAs(t, err, &validationErr)

	_, err = decorated.SendForward(scheduleTestContext(), domainSend.ForwardRequest{
		ScheduleOptions: options, Phone: "628123456789", MessageID: "3EB0MISSING",
	})
	require.ErrorAs(t, err, &validationErr)
	require.ErrorContains(t, err, "not found")
}
