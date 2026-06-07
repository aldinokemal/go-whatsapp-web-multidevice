package whatsapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
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

func TestSendMessageWithReachoutRetry_WaitsForPrivacyTokenBeforeRetry(t *testing.T) {
	origSend := sendMessageFn
	origSub := subscribePresenceFn
	defer func() {
		sendMessageFn = origSend
		subscribePresenceFn = origSub
	}()

	recipient := types.JID{User: "12345", Server: types.DefaultUserServer}
	tokenStore := &memoryPrivacyTokenStore{}
	client := &whatsmeow.Client{
		Store: &store.Device{
			PrivacyTokens: tokenStore,
		},
	}

	var sendCalls int
	sendMessageFn = func(ctx context.Context, client *whatsmeow.Client, recipient types.JID, _ *waE2E.Message) (whatsmeow.SendResponse, error) {
		sendCalls++
		if sendCalls == 1 {
			return whatsmeow.SendResponse{}, reachoutErr()
		}
		token, err := client.Store.PrivacyTokens.GetPrivacyToken(ctx, recipient)
		if err != nil {
			return whatsmeow.SendResponse{}, err
		}
		if token == nil {
			return whatsmeow.SendResponse{}, reachoutErr()
		}
		return whatsmeow.SendResponse{ID: "msg-after-token"}, nil
	}
	subscribePresenceFn = func(context.Context, *whatsmeow.Client, types.JID) error {
		go func() {
			time.Sleep(25 * time.Millisecond)
			_ = tokenStore.PutPrivacyTokens(context.Background(), store.PrivacyToken{
				User:      recipient,
				Token:     []byte("token"),
				Timestamp: time.Now(),
			})
		}()
		return nil
	}

	resp, err := SendMessageWithReachoutRetry(context.Background(), client, recipient, &waE2E.Message{})
	if err != nil {
		t.Fatalf("expected retry to wait for token and succeed, got %v", err)
	}
	if resp.ID != "msg-after-token" {
		t.Fatalf("expected retry response after token, got %q", resp.ID)
	}
	if sendCalls != 2 {
		t.Fatalf("expected 2 send attempts, got %d", sendCalls)
	}
}

func TestSendMessageWithReachoutRetry_RetriesWhenPrivacyTokenNeverAppears(t *testing.T) {
	origSend := sendMessageFn
	origSub := subscribePresenceFn
	origWait := reachoutPrivacyTokenWait
	origPoll := reachoutPrivacyTokenPollPeriod
	defer func() {
		sendMessageFn = origSend
		subscribePresenceFn = origSub
		reachoutPrivacyTokenWait = origWait
		reachoutPrivacyTokenPollPeriod = origPoll
	}()

	reachoutPrivacyTokenWait = 20 * time.Millisecond
	reachoutPrivacyTokenPollPeriod = 5 * time.Millisecond

	recipient := types.JID{User: "12345", Server: types.DefaultUserServer}
	client := &whatsmeow.Client{
		Store: &store.Device{
			PrivacyTokens: &memoryPrivacyTokenStore{},
		},
	}

	var sendCalls int
	sendMessageFn = func(context.Context, *whatsmeow.Client, types.JID, *waE2E.Message) (whatsmeow.SendResponse, error) {
		sendCalls++
		return whatsmeow.SendResponse{}, reachoutErr()
	}
	subscribePresenceFn = func(context.Context, *whatsmeow.Client, types.JID) error {
		return nil
	}

	_, err := SendMessageWithReachoutRetry(context.Background(), client, recipient, &waE2E.Message{})
	if !errors.Is(err, whatsmeow.ErrServerReturnedError) {
		t.Fatalf("expected original reachout error after retry, got %v", err)
	}
	if sendCalls != 2 {
		t.Fatalf("expected 2 send attempts, got %d", sendCalls)
	}
}

type memoryPrivacyTokenStore struct {
	mu     sync.RWMutex
	tokens map[types.JID]store.PrivacyToken
}

func (s *memoryPrivacyTokenStore) PutPrivacyTokens(_ context.Context, tokens ...store.PrivacyToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = make(map[types.JID]store.PrivacyToken)
	}
	for _, token := range tokens {
		s.tokens[token.User.ToNonAD()] = token
	}
	return nil
}

func (s *memoryPrivacyTokenStore) GetPrivacyToken(_ context.Context, user types.JID) (*store.PrivacyToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.tokens[user.ToNonAD()]
	if !ok {
		return nil, nil
	}
	return &token, nil
}

func (s *memoryPrivacyTokenStore) DeleteExpiredPrivacyTokens(context.Context, time.Time) (int64, error) {
	return 0, nil
}
