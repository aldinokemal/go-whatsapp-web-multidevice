package whatsapp

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
)

// WhatsApp grants newsletter live updates for a short, server-chosen duration
// (a few minutes). Renewing before that keeps newsletter.message webhooks
// (view, forward and reaction counters) flowing without gaps.
const (
	liveUpdatesRenewFraction  = 0.8
	liveUpdatesMinRenewDelay  = 5 * time.Second
	liveUpdatesRetryDelay     = 30 * time.Second
	liveUpdatesMaxRetryDelay  = 5 * time.Minute
	liveUpdatesRequestTimeout = 15 * time.Second
)

type liveUpdatesClient interface {
	IsConnected() bool
	NewsletterSubscribeLiveUpdates(ctx context.Context, jid types.JID) (time.Duration, error)
}

// liveUpdatesClientSource resolves the device's current client on every
// renewal, because reconnects and re-pairing replace the whatsmeow client.
type liveUpdatesClientSource func() liveUpdatesClient

// LiveUpdatesStatus is a snapshot of one subscription.
type LiveUpdatesStatus struct {
	NewsletterJID types.JID
	Duration      time.Duration
	ExpiresAt     time.Time
	RenewedAt     time.Time
	LastError     string
}

type liveUpdatesSubscription struct {
	jid    types.JID
	source liveUpdatesClientSource
	stop   chan struct{}
	kick   chan struct{}

	mu        sync.Mutex
	duration  time.Duration
	expiresAt time.Time
	renewedAt time.Time
	lastError string
}

type newsletterLiveUpdatesManager struct {
	mu   sync.Mutex
	subs map[string]map[string]*liveUpdatesSubscription // device ID -> newsletter JID -> subscription
	now  func() time.Time
}

var newsletterLiveUpdates = newNewsletterLiveUpdatesManager()

func newNewsletterLiveUpdatesManager() *newsletterLiveUpdatesManager {
	return &newsletterLiveUpdatesManager{
		subs: make(map[string]map[string]*liveUpdatesSubscription),
		now:  time.Now,
	}
}

// SubscribeNewsletterLiveUpdates subscribes the device to live updates of a
// newsletter and keeps the subscription renewed in the background until
// UnsubscribeNewsletterLiveUpdates is called or the device is removed.
// Subscriptions are kept in memory only and do not survive a restart.
func SubscribeNewsletterLiveUpdates(ctx context.Context, instance *DeviceInstance, jid types.JID) (LiveUpdatesStatus, error) {
	if instance == nil {
		return LiveUpdatesStatus{}, fmt.Errorf("device is required")
	}
	return newsletterLiveUpdates.subscribe(ctx, instance.ID(), jid, instanceLiveUpdatesSource(instance))
}

// UnsubscribeNewsletterLiveUpdates stops renewing a subscription. The server
// keeps sending updates until the current grant expires.
func UnsubscribeNewsletterLiveUpdates(deviceID string, jid types.JID) bool {
	return newsletterLiveUpdates.unsubscribe(deviceID, jid)
}

// ListNewsletterLiveUpdates returns the device's active subscriptions.
func ListNewsletterLiveUpdates(deviceID string) []LiveUpdatesStatus {
	return newsletterLiveUpdates.list(deviceID)
}

func instanceLiveUpdatesSource(instance *DeviceInstance) liveUpdatesClientSource {
	return func() liveUpdatesClient {
		client := instance.GetClient()
		if client == nil {
			return nil
		}
		return client
	}
}

func (m *newsletterLiveUpdatesManager) subscribe(ctx context.Context, deviceID string, jid types.JID, source liveUpdatesClientSource) (LiveUpdatesStatus, error) {
	client := source()
	if client == nil || !client.IsConnected() {
		return LiveUpdatesStatus{}, fmt.Errorf("device %s is not connected", deviceID)
	}
	duration, err := client.NewsletterSubscribeLiveUpdates(ctx, jid)
	if err != nil {
		return LiveUpdatesStatus{}, fmt.Errorf("failed to subscribe to newsletter live updates: %w", err)
	}

	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing := m.subs[deviceID][jid.String()]; existing != nil {
		existing.mu.Lock()
		existing.source = source
		existing.mu.Unlock()
		existing.recordSuccess(now, duration)
		existing.signal()
		return existing.status(), nil
	}

	sub := &liveUpdatesSubscription{
		jid:    jid,
		source: source,
		stop:   make(chan struct{}),
		kick:   make(chan struct{}, 1),
	}
	sub.recordSuccess(now, duration)
	if m.subs[deviceID] == nil {
		m.subs[deviceID] = make(map[string]*liveUpdatesSubscription)
	}
	m.subs[deviceID][jid.String()] = sub
	go m.renewLoop(deviceID, sub)

	return sub.status(), nil
}

func (m *newsletterLiveUpdatesManager) unsubscribe(deviceID string, jid types.JID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sub := m.subs[deviceID][jid.String()]
	if sub == nil {
		return false
	}
	close(sub.stop)
	delete(m.subs[deviceID], jid.String())
	if len(m.subs[deviceID]) == 0 {
		delete(m.subs, deviceID)
	}
	return true
}

func (m *newsletterLiveUpdatesManager) stopDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subs[deviceID] {
		close(sub.stop)
	}
	delete(m.subs, deviceID)
}

// renewDevice renews every subscription of a device right away, e.g. after a
// reconnect, when the server-side grants are gone.
func (m *newsletterLiveUpdatesManager) renewDevice(deviceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range m.subs[deviceID] {
		sub.signal()
	}
}

func (m *newsletterLiveUpdatesManager) list(deviceID string) []LiveUpdatesStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	statuses := make([]LiveUpdatesStatus, 0, len(m.subs[deviceID]))
	for _, sub := range m.subs[deviceID] {
		statuses = append(statuses, sub.status())
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].NewsletterJID.String() < statuses[j].NewsletterJID.String()
	})
	return statuses
}

func (m *newsletterLiveUpdatesManager) renewLoop(deviceID string, sub *liveUpdatesSubscription) {
	failures := 0
	delay := sub.nextRenewDelay(m.now())
	for {
		timer := time.NewTimer(delay)
		select {
		case <-sub.stop:
			timer.Stop()
			return
		case <-sub.kick:
			timer.Stop()
		case <-timer.C:
		}

		err := m.renew(sub)
		select {
		case <-sub.stop:
			return
		default:
		}
		if err != nil {
			failures++
			delay = liveUpdatesBackoff(failures)
			sub.recordFailure(err)
			logrus.Warnf("Newsletter live updates renewal failed for %s on device %s (retry in %s): %v", sub.jid, deviceID, delay, err)
			continue
		}
		failures = 0
		delay = sub.nextRenewDelay(m.now())
	}
}

func (m *newsletterLiveUpdatesManager) renew(sub *liveUpdatesSubscription) error {
	client := sub.currentSource()()
	if client == nil || !client.IsConnected() {
		return fmt.Errorf("device is not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveUpdatesRequestTimeout)
	defer cancel()
	duration, err := client.NewsletterSubscribeLiveUpdates(ctx, sub.jid)
	if err != nil {
		return err
	}
	sub.recordSuccess(m.now(), duration)
	return nil
}

func liveUpdatesBackoff(failures int) time.Duration {
	delay := liveUpdatesRetryDelay
	for i := 1; i < failures && delay < liveUpdatesMaxRetryDelay; i++ {
		delay *= 2
	}
	return min(delay, liveUpdatesMaxRetryDelay)
}

func (s *liveUpdatesSubscription) signal() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *liveUpdatesSubscription) currentSource() liveUpdatesClientSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source
}

func (s *liveUpdatesSubscription) recordSuccess(now time.Time, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.duration = duration
	s.renewedAt = now
	s.expiresAt = now.Add(duration)
	s.lastError = ""
}

func (s *liveUpdatesSubscription) recordFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err.Error()
}

func (s *liveUpdatesSubscription) nextRenewDelay(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	delay := time.Duration(float64(s.duration) * liveUpdatesRenewFraction)
	delay -= now.Sub(s.renewedAt)
	return max(delay, liveUpdatesMinRenewDelay)
}

func (s *liveUpdatesSubscription) status() LiveUpdatesStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return LiveUpdatesStatus{
		NewsletterJID: s.jid,
		Duration:      s.duration,
		ExpiresAt:     s.expiresAt,
		RenewedAt:     s.renewedAt,
		LastError:     s.lastError,
	}
}
