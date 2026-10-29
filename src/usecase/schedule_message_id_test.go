package usecase

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/stretchr/testify/require"
)

// scheduleIDSendStub records the WhatsApp message id each scheduled send was
// asked to use and fails as the test dictates.
type scheduleIDSendStub struct {
	domainSend.ISendUsecase
	ids  []string
	fail func(attempt int) error
}

func (s *scheduleIDSendStub) SendText(ctx context.Context, _ domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	id := whatsapp.SendMessageIDFromContext(ctx)
	s.ids = append(s.ids, id)
	if s.fail != nil {
		if err := s.fail(len(s.ids)); err != nil {
			return domainSend.GenericResponse{}, err
		}
	}
	return domainSend.GenericResponse{MessageID: id}, nil
}

// reclaimScheduleTestJob leases a stored job again, as the worker does once
// its retry comes due.
func reclaimScheduleTestJob(t *testing.T, repo domainChatStorage.IChatStorageRepository, token string) *domainChatStorage.ScheduledSend {
	t.Helper()
	far := scheduleTestNow.Add(365 * 24 * time.Hour)
	claimed, err := repo.ClaimNextScheduledSend(far, far.Add(2*time.Minute), token)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	return claimed
}

// TestScheduledRetryReusesTheMessageID pins the fix: the scheduler retries
// after any send error, including one where WhatsApp accepted the message and
// only the acknowledgment was lost. The retry used to go out under a new
// random id. Reusing the identity avoids creating a fresh message on retry;
// it does not establish exactly-once delivery.
func TestScheduledRetryReusesTheMessageID(t *testing.T) {
	base := &scheduleIDSendStub{fail: func(attempt int) error {
		if attempt <= 2 {
			return errors.New("context deadline exceeded: acknowledgment lost")
		}
		return nil
	}}
	service, repo := newScheduleTestService(t, base)
	claimed := claimScheduleTestJob(t, service, repo, newScheduleTestJob("job", "once", scheduleTestNow.Add(-time.Minute)))

	require.Error(t, service.deliver(scheduleTestContext(), claimed))
	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, 1, stored.Attempts)
	require.NotEmpty(t, stored.PendingMessageID, "the id must be stored before the first attempt")

	require.Error(t, service.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, repo, "lease-2")))
	require.NoError(t, service.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, repo, "lease-3")))

	require.Len(t, base.ids, 3)
	require.Regexp(t, regexp.MustCompile(`^3EB0[0-9A-F]+$`), base.ids[0])
	require.Equal(t, base.ids[0], base.ids[1], "a retry must reuse the id of the first attempt")
	require.Equal(t, base.ids[0], base.ids[2], "a retry must reuse the id of the first attempt")

	stored, err = repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusCompleted, stored.Status)
	require.Equal(t, base.ids[0], stored.LastMessageID)
	require.Empty(t, stored.PendingMessageID)
}

// TestScheduledOccurrencesGetTheirOwnMessageID: the id belongs to one
// occurrence. The next occurrence of a recurring schedule must not reuse it,
// or WhatsApp would drop the new message as a duplicate of the old one.
func TestScheduledOccurrencesGetTheirOwnMessageID(t *testing.T) {
	base := &scheduleIDSendStub{}
	service, repo := newScheduleTestService(t, base)
	claimed := claimScheduleTestJob(t, service, repo, newScheduleTestJob("job", "daily", scheduleTestNow.Add(-time.Minute)))

	require.NoError(t, service.deliver(scheduleTestContext(), claimed))
	require.NoError(t, service.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, repo, "lease-2")))

	require.Len(t, base.ids, 2)
	require.NotEmpty(t, base.ids[0])
	require.NotEqual(t, base.ids[0], base.ids[1])
}

// TestScheduledSkippedOccurrenceDoesNotLeakItsMessageID: an occurrence that
// is given up (attempt budget spent) may have reached WhatsApp. The occurrence
// after it starts with a fresh id.
func TestScheduledSkippedOccurrenceDoesNotLeakItsMessageID(t *testing.T) {
	base := &scheduleIDSendStub{fail: func(attempt int) error {
		if attempt == 1 {
			return errors.New("acknowledgment lost")
		}
		return nil
	}}
	service, repo := newScheduleTestService(t, base)
	job := newScheduleTestJob("job", "daily", scheduleTestNow.Add(-time.Minute))
	job.Attempts = scheduleMaxAttempts - 1
	claimed := claimScheduleTestJob(t, service, repo, job)

	// The last allowed attempt fails: the occurrence is skipped.
	require.Error(t, service.deliver(scheduleTestContext(), claimed))
	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, 0, stored.Attempts)
	require.Empty(t, stored.PendingMessageID)

	require.NoError(t, service.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, repo, "lease-2")))
	require.Len(t, base.ids, 2)
	require.NotEqual(t, base.ids[0], base.ids[1])
}

// TestScheduledOfflineWaitKeepsTheMessageID: waiting for the device between
// two attempts of an occurrence must not change the id it is retried under.
func TestScheduledOfflineWaitKeepsTheMessageID(t *testing.T) {
	service, repo := newScheduleTestService(t, &scheduleIDSendStub{})
	job := newScheduleTestJob("job", "once", scheduleTestNow.Add(-time.Minute))
	job.Attempts = 2
	job.PendingMessageID = "3EB0FIRSTATTEMPT"
	claimed := claimScheduleTestJob(t, service, repo, job)

	// No device manager: the device counts as offline.
	require.NoError(t, service.processJob(context.Background(), claimed))

	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, 2, stored.Attempts)
	require.Equal(t, "3EB0FIRSTATTEMPT", stored.PendingMessageID)
}

// scheduleFailingPinRepo fails to store the occurrence's message id.
type scheduleFailingPinRepo struct {
	domainChatStorage.IChatStorageRepository
}

func (r scheduleFailingPinRepo) SetScheduledSendPendingAttempt(string, string, string, []byte) (bool, error) {
	return false, errors.New("database is locked")
}

// TestScheduledSendNeverGoesOutUnderAnUnstoredID: if the id cannot be stored,
// nothing is sent — a later attempt could not repeat it — and the wait costs
// no attempt.
func TestScheduledSendNeverGoesOutUnderAnUnstoredID(t *testing.T) {
	base := &scheduleIDSendStub{}
	service, repo := newScheduleTestService(t, base)
	claimed := claimScheduleTestJob(t, service, repo, newScheduleTestJob("job", "once", scheduleTestNow.Add(-time.Minute)))
	service.repo = scheduleFailingPinRepo{IChatStorageRepository: repo}

	require.Error(t, service.deliver(scheduleTestContext(), claimed))

	require.Empty(t, base.ids, "nothing may be sent under an id that was not stored")
	stored, err := repo.GetScheduledSend("device-a", "job")
	require.NoError(t, err)
	require.Equal(t, scheduleStatusActive, stored.Status)
	require.Equal(t, 0, stored.Attempts)
	require.True(t, scheduleTestNow.Add(30*time.Second).Equal(stored.NextRunAt))
}
