package usecase

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func scheduledPollClient(t *testing.T, jid types.JID) *whatsmeow.Client {
	t.Helper()
	ctx := context.Background()
	container, err := sqlstore.New(ctx, sqlite.DriverName, sqlite.FormatChatStorageURI(filepath.Join(t.TempDir(), "secrets.db"), false, true), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Close()) })
	device := container.NewDevice()
	device.ID = &jid
	device.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte{0}, AccountSignatureKey: make([]byte, 32), AccountSignature: make([]byte, 64), DeviceSignature: make([]byte, 64)}
	require.NoError(t, device.Save(ctx))
	return whatsmeow.NewClient(device, nil)
}

type scheduledPollSender struct {
	domainSend.ISendUsecase
	t        *testing.T
	client   *whatsmeow.Client
	chat     types.JID
	fail     bool
	ids      []string
	payloads []*waE2E.Message
}

func (s *scheduledPollSender) SendPoll(ctx context.Context, request domainSend.PollRequest) (domainSend.GenericResponse, error) {
	id := whatsapp.SendMessageIDFromContext(ctx)
	// Exercise the production helper and the pinned whatsmeow builder. Like
	// SendMessage, store the secret before the simulated failed network send.
	payload := buildPollCreation(ctx, s.client, request)
	require.NoError(s.t, s.client.Store.MsgSecrets.PutMessageSecret(ctx, s.chat, *s.client.Store.ID, id, payload.MessageContextInfo.MessageSecret))
	s.ids = append(s.ids, id)
	s.payloads = append(s.payloads, payload)
	if s.fail {
		return domainSend.GenericResponse{}, errors.New("network failed after storing poll secret")
	}
	return domainSend.GenericResponse{MessageID: id}, nil
}

func TestScheduledPollRetryDecryptsVotesWithDeliveredSecret(t *testing.T) {
	ctx := context.Background()
	creator, voter := types.NewJID("628111", types.DefaultUserServer), types.NewJID("628222", types.DefaultUserServer)
	chat := types.NewJID("120363000000", types.GroupServer)
	sender, recipient := scheduledPollClient(t, creator), scheduledPollClient(t, voter)
	base := &scheduledPollSender{t: t, client: sender, chat: chat, fail: true}
	path, mediaRoot := filepath.Join(t.TempDir(), "scheduler.db"), t.TempDir()
	first, repo := newScheduleTestServiceAt(t, path, mediaRoot, base, scheduleTestNow)
	job := newScheduleTestJob("poll", "daily", scheduleTestNow.Add(-time.Minute))
	job.MessageType, job.PayloadJSON = "poll", `{"phone":"120363000000@g.us","question":"Lunch?","options":["Pizza","Sushi"],"max_answer":1}`
	claimed := claimScheduleTestJob(t, first, repo, job)
	require.Error(t, first.deliver(scheduleTestContext(), claimed))
	stored, err := repo.GetScheduledSend("device-a", "poll")
	require.NoError(t, err)
	firstSecret := append([]byte(nil), stored.PendingPollSecret...)
	require.Len(t, firstSecret, 32)
	require.Equal(t, firstSecret, base.payloads[0].MessageContextInfo.MessageSecret)
	// An offline wait and a subsequent interrupted lease both preserve the secret.
	offline, err := repo.ClaimNextScheduledSend(scheduleTestNow.Add(time.Minute), scheduleTestNow.Add(2*time.Minute), "offline")
	require.NoError(t, err)
	require.NotNil(t, offline)
	require.NoError(t, first.processJob(ctx, offline))
	crashed, err := repo.ClaimNextScheduledSend(scheduleTestNow.Add(time.Minute), scheduleTestNow.Add(2*time.Minute), "crashed")
	require.NoError(t, err)
	require.NotNil(t, crashed)
	restarted, restartedRepo := newScheduleTestServiceAt(t, path, mediaRoot, base, scheduleTestNow.Add(3*time.Minute))
	restarted.recoverInterrupted()
	base.fail = false
	retry := reclaimScheduleTestJob(t, restartedRepo, "retry")
	require.NoError(t, restarted.deliver(scheduleTestContext(), retry))
	require.Equal(t, base.ids[0], base.ids[1])
	delivered := base.payloads[1].MessageContextInfo.MessageSecret
	require.Equal(t, firstSecret, delivered)
	// The recipient encrypts with what was actually delivered. The sender's
	// SQLite store retains the first insert (ON CONFLICT DO NOTHING).
	require.NoError(t, recipient.Store.MsgSecrets.PutMessageSecret(ctx, chat, creator, base.ids[1], delivered))
	vote, err := recipient.BuildPollVote(ctx, &types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: creator, IsGroup: true}, ID: base.ids[1]}, []string{"Sushi"})
	require.NoError(t, err)
	decrypted, err := sender.DecryptPollVote(ctx, &events.Message{Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: voter, IsGroup: true}, ID: "VOTE"}, Message: vote})
	require.NoError(t, err)
	require.Equal(t, whatsmeow.HashPollOptions([]string{"Sushi"}), decrypted.SelectedOptions)
	stored, err = restartedRepo.GetScheduledSend("device-a", "poll")
	require.NoError(t, err)
	require.Empty(t, stored.PendingPollSecret)
	// A new occurrence has a new ID and secret.
	require.NoError(t, restarted.deliver(scheduleTestContext(), reclaimScheduleTestJob(t, restartedRepo, "next")))
	require.NotEqual(t, base.ids[1], base.ids[2])
	require.NotEqual(t, delivered, base.payloads[2].MessageContextInfo.MessageSecret)
}
