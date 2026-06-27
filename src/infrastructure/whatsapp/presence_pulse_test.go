package whatsapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

type fakePresencePulseSource struct {
	devices []presencePulseDevice
}

func (s fakePresencePulseSource) ListPresencePulseDevices() []presencePulseDevice {
	return s.devices
}

type fakePresencePulseClient struct {
	mu        sync.Mutex
	connected bool
	loggedIn  bool
	calls     []types.Presence
	callCh    chan types.Presence
	sendErr   error
}

func newFakePresencePulseClient() *fakePresencePulseClient {
	return &fakePresencePulseClient{
		connected: true,
		loggedIn:  true,
		callCh:    make(chan types.Presence, 4),
	}
}

func (c *fakePresencePulseClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

func (c *fakePresencePulseClient) IsLoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loggedIn
}

func (c *fakePresencePulseClient) SendPresence(_ context.Context, presence types.Presence) error {
	c.mu.Lock()
	c.calls = append(c.calls, presence)
	callCh := c.callCh
	err := c.sendErr
	c.mu.Unlock()

	if callCh != nil {
		callCh <- presence
	}
	return err
}

func (c *fakePresencePulseClient) setConnected(connected bool) {
	c.mu.Lock()
	c.connected = connected
	c.mu.Unlock()
}

func (c *fakePresencePulseClient) setLoggedIn(loggedIn bool) {
	c.mu.Lock()
	c.loggedIn = loggedIn
	c.mu.Unlock()
}

func (c *fakePresencePulseClient) presenceCalls() []types.Presence {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]types.Presence(nil), c.calls...)
}

func waitForPresenceCall(t *testing.T, ch <-chan types.Presence) types.Presence {
	t.Helper()
	select {
	case presence := <-ch:
		return presence
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for presence call")
		return ""
	}
}

func waitForNoPulseInFlight(t *testing.T, scheduler *presencePulseScheduler, deviceID string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		scheduler.mu.Lock()
		inFlight := scheduler.inFlight[deviceID]
		scheduler.mu.Unlock()
		if !inFlight {
			return
		}

		select {
		case <-deadline:
			t.Fatal("timeout waiting for pulse to finish")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestPresencePulseSendsAvailableThenUnavailable(t *testing.T) {
	client := newFakePresencePulseClient()
	scheduler := newPresencePulseScheduler(fakePresencePulseSource{}, time.Hour, time.Minute, time.Minute)
	scheduler.sleep = func(_ context.Context, duration time.Duration) bool {
		if duration != time.Minute {
			t.Fatalf("sleep duration = %s, want %s", duration, time.Minute)
		}
		return true
	}

	started := scheduler.startPulseIfDue(context.Background(), presencePulseDevice{
		id:     "device-1",
		client: client,
	})
	if !started {
		t.Fatal("expected pulse to start")
	}

	if got := waitForPresenceCall(t, client.callCh); got != types.PresenceAvailable {
		t.Fatalf("first presence = %q, want %q", got, types.PresenceAvailable)
	}
	if got := waitForPresenceCall(t, client.callCh); got != types.PresenceUnavailable {
		t.Fatalf("second presence = %q, want %q", got, types.PresenceUnavailable)
	}
}

func TestPresencePulseSkipsUnavailableDevices(t *testing.T) {
	nilClient := presencePulseDevice{id: "nil-client"}
	disconnected := newFakePresencePulseClient()
	disconnected.setConnected(false)
	loggedOut := newFakePresencePulseClient()
	loggedOut.setLoggedIn(false)

	scheduler := newPresencePulseScheduler(fakePresencePulseSource{
		devices: []presencePulseDevice{
			nilClient,
			{id: "disconnected", client: disconnected},
			{id: "logged-out", client: loggedOut},
		},
	}, time.Hour, time.Minute, time.Minute)
	scheduler.runDuePulses(context.Background())

	if len(disconnected.presenceCalls()) != 0 {
		t.Fatalf("disconnected device received presence calls: %v", disconnected.presenceCalls())
	}
	if len(loggedOut.presenceCalls()) != 0 {
		t.Fatalf("logged-out device received presence calls: %v", loggedOut.presenceCalls())
	}
}

func TestPresencePulsePreventsDuplicateInFlightPulse(t *testing.T) {
	client := newFakePresencePulseClient()
	blockSleep := make(chan struct{})
	scheduler := newPresencePulseScheduler(fakePresencePulseSource{}, time.Hour, time.Minute, time.Minute)
	scheduler.sleep = func(ctx context.Context, _ time.Duration) bool {
		select {
		case <-blockSleep:
			return true
		case <-ctx.Done():
			return false
		}
	}
	device := presencePulseDevice{id: "device-1", client: client}

	if !scheduler.startPulseIfDue(context.Background(), device) {
		t.Fatal("expected first pulse to start")
	}
	if got := waitForPresenceCall(t, client.callCh); got != types.PresenceAvailable {
		t.Fatalf("first presence = %q, want %q", got, types.PresenceAvailable)
	}
	if scheduler.startPulseIfDue(context.Background(), device) {
		t.Fatal("expected duplicate pulse to be skipped while first pulse is in flight")
	}

	close(blockSleep)
	_ = waitForPresenceCall(t, client.callCh)
}

func TestPresencePulseDoesNotRunBeforeInterval(t *testing.T) {
	client := newFakePresencePulseClient()
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	scheduler := newPresencePulseScheduler(fakePresencePulseSource{}, 24*time.Hour, time.Minute, time.Minute)
	scheduler.now = func() time.Time { return now }
	scheduler.sleep = func(context.Context, time.Duration) bool { return true }
	device := presencePulseDevice{id: "device-1", client: client}

	if !scheduler.startPulseIfDue(context.Background(), device) {
		t.Fatal("expected first pulse to start")
	}
	_ = waitForPresenceCall(t, client.callCh)
	_ = waitForPresenceCall(t, client.callCh)
	waitForNoPulseInFlight(t, scheduler, device.id)

	now = now.Add(24*time.Hour - time.Second)
	if scheduler.startPulseIfDue(context.Background(), device) {
		t.Fatal("expected pulse before interval to be skipped")
	}
}

func TestPresencePulseCanRetryAfterAvailablePresenceFails(t *testing.T) {
	client := newFakePresencePulseClient()
	sendErr := errors.New("send failed")
	client.sendErr = sendErr

	scheduler := newPresencePulseScheduler(fakePresencePulseSource{}, 24*time.Hour, time.Minute, time.Minute)
	scheduler.sleep = func(context.Context, time.Duration) bool { return true }
	device := presencePulseDevice{id: "device-1", client: client}

	if !scheduler.startPulseIfDue(context.Background(), device) {
		t.Fatal("expected first pulse to start")
	}
	if got := waitForPresenceCall(t, client.callCh); got != types.PresenceAvailable {
		t.Fatalf("first presence = %q, want %q", got, types.PresenceAvailable)
	}
	waitForNoPulseInFlight(t, scheduler, device.id)

	client.sendErr = nil
	if !scheduler.startPulseIfDue(context.Background(), device) {
		t.Fatal("expected retry after failed available presence")
	}
	if got := waitForPresenceCall(t, client.callCh); got != types.PresenceAvailable {
		t.Fatalf("retry presence = %q, want %q", got, types.PresenceAvailable)
	}
}

func TestPresencePulseSkipsUnavailableWhenClientDisconnectsDuringPulse(t *testing.T) {
	client := newFakePresencePulseClient()
	scheduler := newPresencePulseScheduler(fakePresencePulseSource{}, time.Hour, time.Minute, time.Minute)
	scheduler.sleep = func(context.Context, time.Duration) bool {
		client.setConnected(false)
		return true
	}

	if !scheduler.startPulseIfDue(context.Background(), presencePulseDevice{
		id:     "device-1",
		client: client,
	}) {
		t.Fatal("expected pulse to start")
	}

	if got := waitForPresenceCall(t, client.callCh); got != types.PresenceAvailable {
		t.Fatalf("first presence = %q, want %q", got, types.PresenceAvailable)
	}

	select {
	case presence := <-client.callCh:
		t.Fatalf("unexpected second presence call: %q", presence)
	case <-time.After(100 * time.Millisecond):
	}
}
