package owners

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"go.mau.fi/whatsmeow/types"
)

type PairedDevice struct{ ID, JID string }

// PairSource is the device manager as the guard sees it. Paired lists the
// devices that are logged in, each with its phone-number JID (Store.ID, not
// the LID). Once Logout returns nil, Paired must no longer list that device.
type PairSource interface {
	Paired() []PairedDevice
	Logout(ctx context.Context, deviceID string) error
}

// OwnerLookup is the part of *store.Store the guard needs.
type OwnerLookup interface {
	OwnerOf(ctx context.Context, deviceID string) (string, error)
	Audit(ctx context.Context, e store.AuditEvent) error
	RecordRefusal(ctx context.Context, deviceID, reason string) error
}

// guardMu serialises the guard so two links of the same number completing at
// the same moment cannot each see the other and both be logged out: the first
// to run is refused, and the second then finds no other link.
var guardMu sync.Mutex

// GuardDuplicatePhone must be called once each time a device finishes pairing
// (on pair success only, never on reconnect). If another user's device is
// already linked to the same WhatsApp number, the new link (deviceID) is
// logged out and audited, and the first owner keeps the number.
//
// It fails closed. A new link is refused when its number cannot be read (a
// JID that does not parse, or one on any server other than s.whatsapp.net,
// such as @lid), when a linked sibling's JID cannot be read, or when an owner
// lookup fails for any reason other than store.ErrNotFound. Only a device with
// no owner row is treated as "not WTF's" and left alone.
//
// refused reports whether deviceID was refused. A non-nil err with
// refused == true means the link was refused but something failed, possibly
// the Logout itself. In that case the device may still be linked, and the
// caller must retry the logout or purge the device.
func GuardDuplicatePhone(ctx context.Context, src PairSource, st OwnerLookup, deviceID string) (refused bool, err error) {
	guardMu.Lock()
	defer guardMu.Unlock()

	newOwner, err := st.OwnerOf(ctx, deviceID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil // not a WTF-owned device; nothing to guard
	}
	if err != nil {
		return refuse(ctx, src, st, "", deviceID, "device_link_refused_lookup_failed", fmt.Errorf("owner of %s: %w", deviceID, err))
	}

	paired := src.Paired()
	number, found := "", false
	for _, d := range paired {
		if d.ID != deviceID {
			continue
		}
		found = true
		n, ok := phoneNumber(d.JID)
		if !ok {
			return refuse(ctx, src, st, newOwner, deviceID, "device_link_refused_unreadable_number", nil)
		}
		number = n
	}
	if !found {
		return false, nil // no longer linked; nothing to keep or refuse
	}

	for _, d := range paired {
		if d.ID == deviceID || d.JID == "" {
			continue
		}
		n, ok := phoneNumber(d.JID)
		if !ok {
			return refuse(ctx, src, st, newOwner, deviceID, "device_link_refused_unreadable_number", nil)
		}
		if n != number {
			continue
		}
		owner, err := st.OwnerOf(ctx, d.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return refuse(ctx, src, st, newOwner, deviceID, "device_link_refused_lookup_failed", fmt.Errorf("owner of %s: %w", d.ID, err))
		}
		if owner != newOwner {
			return refuse(ctx, src, st, newOwner, deviceID, "device_link_refused_duplicate", nil)
		}
	}
	return false, nil
}

func refuse(ctx context.Context, src PairSource, st OwnerLookup, userID, deviceID, action string, cause error) (bool, error) {
	_ = st.Audit(ctx, store.AuditEvent{UserID: userID, Action: action, DeviceID: deviceID})
	// The slot keeps the reason, so its row can say why it is not linked
	// until the next accepted link clears it.
	reason := store.RefusalUnreadable
	if action == "device_link_refused_duplicate" {
		reason = store.RefusalDuplicate
	}
	_ = st.RecordRefusal(ctx, deviceID, reason)
	if err := src.Logout(ctx, deviceID); err != nil {
		return true, errors.Join(cause, fmt.Errorf("logout refused device %s: %w", deviceID, err))
	}
	return true, cause
}

// phoneNumber turns "27825550999:12@s.whatsapp.net" into "27825550999". Any
// JID that is not a phone-number JID reports ok == false.
func phoneNumber(jid string) (string, bool) {
	j, err := types.ParseJID(jid)
	if err != nil || j.Server != types.DefaultUserServer || j.User == "" {
		return "", false
	}
	return j.User, true
}

// PhoneNumberReady reports whether jid is a phone-number JID whose number can
// be read right now. It is the cheap, store-free, network-free signal a
// caller can poll for right after a pairing, to find out whether the
// just-paired device's own JID has settled into a readable shape yet, before
// running the full duplicate guard.
func PhoneNumberReady(jid string) bool {
	_, ok := phoneNumber(jid)
	return ok
}
