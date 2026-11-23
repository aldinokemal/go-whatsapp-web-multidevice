package owners

import (
	"context"
	"errors"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/sirupsen/logrus"
)

// StaleSlotAge is how long a reserved slot may sit without ever being
// scanned before the sweeper frees it. A linking session (QR or pairing
// code) lasts two minutes, so an hour is long past any real attempt.
const StaleSlotAge = time.Hour

// StaleSlotSweeper frees link slots that were reserved and never scanned, so
// an abandoned "Link WhatsApp" cannot hold a place against a user's limit or
// the server's cap. A slot that ever held a number (connected, signed out
// from the phone, or refused) is never touched; the owner's own record of it
// stays. Each slot it frees is audited against its owner.
type StaleSlotSweeper struct {
	Store *store.Store
	// Devices is the inner (unscoped) device usecase: RemoveDevice purges the
	// slot from the device manager. ErrDeviceNotFound counts as done.
	Devices interface {
		RemoveDevice(ctx context.Context, deviceID string) error
	}
	// Live lists the device manager's slots with their current JID. A slot
	// with a JID right now is linked whatever accounts.db has recorded yet,
	// and is skipped.
	Live   func() []PairedDevice
	MaxAge time.Duration    // 0 means StaleSlotAge
	Now    func() time.Time // nil means time.Now
}

func (s *StaleSlotSweeper) hasJID(id string) bool {
	if s.Live == nil {
		return false
	}
	for _, d := range s.Live() {
		if d.ID == id {
			return d.JID != ""
		}
	}
	return false
}

// SweepOnce frees every never-scanned slot older than MaxAge and returns how
// many it freed. It carries on past one slot's failure.
func (s *StaleSlotSweeper) SweepOnce(ctx context.Context) (int, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	age := s.MaxAge
	if age <= 0 {
		age = StaleSlotAge
	}
	cutoff := now().Add(-age)
	stale, err := s.Store.NeverLinkedSlotsBefore(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	freed := 0
	var errs error
	for _, sl := range stale {
		if s.hasJID(sl.DeviceID) {
			continue
		}
		// Claim first: the delete re-checks, in one statement, that nobody
		// has linked it since the listing.
		ok, err := s.Store.ExpireNeverLinkedSlot(ctx, sl.DeviceID, cutoff)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if !ok {
			continue
		}
		// Linked in the instant between the check and the claim: give it back.
		if s.hasJID(sl.DeviceID) {
			if err := s.Store.SetOwner(ctx, sl.DeviceID, sl.UserID); err != nil {
				errs = errors.Join(errs, err)
			}
			continue
		}
		if err := s.Devices.RemoveDevice(ctx, sl.DeviceID); err != nil && !errors.Is(err, pkgError.ErrDeviceNotFound) {
			logrus.WithError(err).Warnf("[WTF] stale slot %s: freed its place, but the device manager kept it", sl.DeviceID)
		}
		_ = s.Store.Audit(ctx, store.AuditEvent{UserID: sl.UserID, Action: "device_slot_expired", DeviceID: sl.DeviceID})
		freed++
	}
	return freed, errs
}

// Run sweeps now and then every interval until ctx ends.
func (s *StaleSlotSweeper) Run(ctx context.Context, every time.Duration) {
	sweep := func() {
		if n, err := s.SweepOnce(ctx); err != nil {
			logrus.WithError(err).Warn("[WTF] stale slot sweep")
		} else if n > 0 {
			logrus.Infof("[WTF] freed %d link slot(s) that were never scanned", n)
		}
	}
	sweep()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
	}
}
