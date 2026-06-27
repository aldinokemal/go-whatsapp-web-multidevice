package whatsapp

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
)

const presencePulseCheckInterval = time.Minute

type presencePulseClient interface {
	IsConnected() bool
	IsLoggedIn() bool
	SendPresence(context.Context, types.Presence) error
}

type presencePulseDevice struct {
	id     string
	client presencePulseClient
}

type presencePulseDeviceSource interface {
	ListPresencePulseDevices() []presencePulseDevice
}

type deviceManagerPresencePulseSource struct {
	manager *DeviceManager
}

func (s deviceManagerPresencePulseSource) ListPresencePulseDevices() []presencePulseDevice {
	if s.manager == nil {
		return nil
	}

	instances := s.manager.ListDevices()
	devices := make([]presencePulseDevice, 0, len(instances))
	for _, instance := range instances {
		if instance == nil {
			continue
		}
		devices = append(devices, presencePulseDevice{
			id:     instance.ID(),
			client: instance.GetClient(),
		})
	}
	return devices
}

type presencePulseScheduler struct {
	source        presencePulseDeviceSource
	interval      time.Duration
	duration      time.Duration
	checkInterval time.Duration
	now           func() time.Time
	sleep         func(context.Context, time.Duration) bool
	mu            sync.Mutex
	lastPulse     map[string]time.Time
	inFlight      map[string]bool
}

func newPresencePulseScheduler(source presencePulseDeviceSource, interval, duration, checkInterval time.Duration) *presencePulseScheduler {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if duration <= 0 {
		duration = 5 * time.Minute
	}
	if checkInterval <= 0 {
		checkInterval = presencePulseCheckInterval
	}

	return &presencePulseScheduler{
		source:        source,
		interval:      interval,
		duration:      duration,
		checkInterval: checkInterval,
		now:           time.Now,
		sleep: func(ctx context.Context, duration time.Duration) bool {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-timer.C:
				return true
			case <-ctx.Done():
				return false
			}
		},
		lastPulse: make(map[string]time.Time),
		inFlight:  make(map[string]bool),
	}
}

func StartPresencePulseScheduler(ctx context.Context, manager *DeviceManager, interval, duration time.Duration) {
	if manager == nil {
		logrus.Warn("[PRESENCE_PULSE] device manager is nil; scheduler not started")
		return
	}

	scheduler := newPresencePulseScheduler(
		deviceManagerPresencePulseSource{manager: manager},
		interval,
		duration,
		presencePulseCheckInterval,
	)
	go scheduler.run(ctx)
}

func (s *presencePulseScheduler) run(ctx context.Context) {
	if s == nil {
		return
	}

	s.runDuePulses(ctx)

	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.runDuePulses(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *presencePulseScheduler) runDuePulses(ctx context.Context) {
	if s == nil || s.source == nil {
		return
	}

	for _, device := range s.source.ListPresencePulseDevices() {
		s.startPulseIfDue(ctx, device)
	}
}

func (s *presencePulseScheduler) startPulseIfDue(ctx context.Context, device presencePulseDevice) bool {
	if s == nil || device.id == "" || !presencePulseDeviceReady(device.client) {
		return false
	}

	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.inFlight[device.id] {
		return false
	}
	if lastPulse, ok := s.lastPulse[device.id]; ok && now.Sub(lastPulse) < s.interval {
		return false
	}

	s.inFlight[device.id] = true

	go s.runPulse(ctx, device)
	return true
}

func (s *presencePulseScheduler) runPulse(ctx context.Context, device presencePulseDevice) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.inFlight[device.id] = false
	}()

	if err := device.client.SendPresence(ctx, types.PresenceAvailable); err != nil {
		logrus.WithError(err).Warnf("[PRESENCE_PULSE] failed to mark device %s as available", device.id)
		return
	}
	s.mu.Lock()
	s.lastPulse[device.id] = s.now()
	s.mu.Unlock()
	logrus.Infof("[PRESENCE_PULSE] marked device %s as available", device.id)

	if !s.sleep(ctx, s.duration) {
		return
	}
	if !presencePulseDeviceReady(device.client) {
		logrus.Infof("[PRESENCE_PULSE] device %s is no longer connected; skipping unavailable presence", device.id)
		return
	}

	if err := device.client.SendPresence(ctx, types.PresenceUnavailable); err != nil {
		logrus.WithError(err).Warnf("[PRESENCE_PULSE] failed to mark device %s as unavailable", device.id)
		return
	}
	logrus.Infof("[PRESENCE_PULSE] marked device %s as unavailable", device.id)
}

func presencePulseDeviceReady(client presencePulseClient) bool {
	return client != nil && client.IsConnected() && client.IsLoggedIn()
}
