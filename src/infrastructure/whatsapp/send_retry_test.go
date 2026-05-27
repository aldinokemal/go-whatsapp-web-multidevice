package whatsapp

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// reachoutErr mimics the error whatsmeow produces for server error 463:
// fmt.Errorf("%w %d", ErrServerReturnedError, 463).
func reachoutErr() error {
	return errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 463"))
}

func nonReachoutServerErr(code string) error {
	return errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error "+code))
}

func TestIsReachoutTimelockError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"wrapped 463", reachoutErr(), true},
		{"wrapped non-463", nonReachoutServerErr("500"), false},
		{"unrelated error", errors.New("network unreachable"), false},
		{"bare 463 string without sentinel", errors.New("error 463"), false},
		{"sentinel but no 463", nonReachoutServerErr("400"), false},
		{"superstring 4631 must not match", nonReachoutServerErr("4631"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReachoutTimelockError(tt.err); got != tt.want {
				t.Fatalf("IsReachoutTimelockError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestSendMessageWithReachoutRetry(t *testing.T) {
	userJID := types.JID{User: "12345", Server: types.DefaultUserServer}
	lidJID := types.JID{User: "12345", Server: types.HiddenUserServer}
	groupJID := types.JID{User: "12345-67890", Server: types.GroupServer}

	type result struct {
		resp whatsmeow.SendResponse
		err  error
	}

	tests := []struct {
		name           string
		recipient      types.JID
		first          result
		second         result
		wantSendCalls  int
		wantSubscribes int
		wantErrIs      error
		wantRespID     string
	}{
		{
			name:           "success on first attempt",
			recipient:      userJID,
			first:          result{resp: whatsmeow.SendResponse{ID: "msg-1"}},
			wantSendCalls:  1,
			wantSubscribes: 0,
			wantRespID:     "msg-1",
		},
		{
			name:           "non-463 error is not retried",
			recipient:      userJID,
			first:          result{err: nonReachoutServerErr("500")},
			wantSendCalls:  1,
			wantSubscribes: 0,
			wantErrIs:      whatsmeow.ErrServerReturnedError,
		},
		{
			name:           "463 on user JID retries and succeeds",
			recipient:      userJID,
			first:          result{err: reachoutErr()},
			second:         result{resp: whatsmeow.SendResponse{ID: "msg-retry"}},
			wantSendCalls:  2,
			wantSubscribes: 1,
			wantRespID:     "msg-retry",
		},
		{
			name:           "463 on LID JID retries and succeeds",
			recipient:      lidJID,
			first:          result{err: reachoutErr()},
			second:         result{resp: whatsmeow.SendResponse{ID: "msg-retry-lid"}},
			wantSendCalls:  2,
			wantSubscribes: 1,
			wantRespID:     "msg-retry-lid",
		},
		{
			name:           "463 on user JID retries and still fails",
			recipient:      userJID,
			first:          result{err: reachoutErr()},
			second:         result{err: reachoutErr()},
			wantSendCalls:  2,
			wantSubscribes: 1,
			wantErrIs:      whatsmeow.ErrServerReturnedError,
		},
		{
			name:           "463 on group JID is not retried",
			recipient:      groupJID,
			first:          result{err: reachoutErr()},
			wantSendCalls:  1,
			wantSubscribes: 0,
			wantErrIs:      whatsmeow.ErrServerReturnedError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origSend := sendMessageFn
			origSub := subscribePresenceFn
			defer func() {
				sendMessageFn = origSend
				subscribePresenceFn = origSub
			}()

			var sendCalls, subscribes int
			sendMessageFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID, _ *waE2E.Message) (whatsmeow.SendResponse, error) {
				sendCalls++
				if sendCalls == 1 {
					return tt.first.resp, tt.first.err
				}
				return tt.second.resp, tt.second.err
			}
			subscribePresenceFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID) error {
				subscribes++
				return nil
			}

			resp, err := SendMessageWithReachoutRetry(context.Background(), nil, tt.recipient, &waE2E.Message{})

			if sendCalls != tt.wantSendCalls {
				t.Errorf("send calls = %d, want %d", sendCalls, tt.wantSendCalls)
			}
			if subscribes != tt.wantSubscribes {
				t.Errorf("subscribe calls = %d, want %d", subscribes, tt.wantSubscribes)
			}
			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Errorf("err = %v, want errors.Is(..., %v)", err, tt.wantErrIs)
				}
			} else {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
				if resp.ID != tt.wantRespID {
					t.Errorf("resp.ID = %q, want %q", resp.ID, tt.wantRespID)
				}
			}
		})
	}
}

func TestSendMessageWithReachoutRetry_SubscribeFailureDoesNotAbortRetry(t *testing.T) {
	origSend := sendMessageFn
	origSub := subscribePresenceFn
	defer func() {
		sendMessageFn = origSend
		subscribePresenceFn = origSub
	}()

	var sendCalls int
	sendMessageFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID, _ *waE2E.Message) (whatsmeow.SendResponse, error) {
		sendCalls++
		if sendCalls == 1 {
			return whatsmeow.SendResponse{}, reachoutErr()
		}
		return whatsmeow.SendResponse{ID: "msg-retry"}, nil
	}
	subscribePresenceFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID) error {
		return errors.New("subscribe boom")
	}

	resp, err := SendMessageWithReachoutRetry(
		context.Background(),
		nil,
		types.JID{User: "12345", Server: types.DefaultUserServer},
		&waE2E.Message{},
	)
	if err != nil {
		t.Fatalf("expected retry to succeed despite subscribe failure, got %v", err)
	}
	if resp.ID != "msg-retry" {
		t.Fatalf("expected retry result, got %q", resp.ID)
	}
	if sendCalls != 2 {
		t.Fatalf("expected 2 send attempts, got %d", sendCalls)
	}
}
