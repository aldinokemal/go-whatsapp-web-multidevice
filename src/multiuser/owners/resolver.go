// Package owners enforces that a user only ever reaches WhatsApp accounts they own.
package owners

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

// ErrDeviceIDRequired is kept for callers that match the sentinel's identity
// (e.g. wiring tests that construct it directly); Resolve itself no longer
// returns this exact value for the "which one?" case - see
// deviceChoiceMessage, which builds the real, per-user message that lists the
// user's own devices by name so there is something to actually say back.
var (
	ErrDeviceIDRequired = pkgError.ValidationError("you have several WhatsApp accounts linked; pass device_id or the X-Device-Id header")
	ErrNoDeviceLinked   = pkgError.ValidationError("link a WhatsApp account first")
	errNoUser           = errors.New("no signed-in user")
)

type DeviceGetter interface {
	GetDevice(id string) (*whatsapp.DeviceInstance, bool)
}

type Resolver struct {
	dm DeviceGetter
	st *store.Store
}

func NewResolver(dm DeviceGetter, st *store.Store) *Resolver { return &Resolver{dm: dm, st: st} }

// Resolve mirrors whatsapp.DeviceManager.ResolveDevice's contract. A device
// that exists but belongs to someone else is reported exactly like one that
// does not exist.
func (r *Resolver) Resolve(ctx context.Context, deviceID string) (*whatsapp.DeviceInstance, string, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, deviceID, errNoUser
	}
	if deviceID == "" {
		ids, err := r.st.DevicesOf(ctx, u.ID)
		if err != nil {
			return nil, "", err
		}
		switch len(ids) {
		case 0:
			return nil, "", ErrNoDeviceLinked
		case 1:
			deviceID = ids[0]
		default:
			labels, err := r.st.LabelsOf(ctx, u.ID)
			if err != nil {
				return nil, "", err
			}
			return nil, "", r.deviceChoiceMessage(ids, labels)
		}
	} else {
		owner, err := r.st.OwnerOf(ctx, deviceID)
		if err != nil || owner != u.ID {
			resolved, matchErr := r.resolveByLabel(ctx, u.ID, deviceID)
			if matchErr != nil {
				return nil, deviceID, matchErr
			}
			deviceID = resolved
		}
	}
	inst, ok := r.dm.GetDevice(deviceID)
	if !ok {
		return nil, deviceID, pkgError.ErrDeviceNotFound
	}
	return inst, deviceID, nil
}

// resolveByLabel is Resolve's fallback once deviceID failed as an exact
// device_id/owner match: deviceID may instead be the human-readable name
// (label) the user gave one of their OWN numbers with SetDeviceLabel. Matched
// case-insensitively, after the same whitespace tidying NormalizeLabel would
// apply to a label when it was saved, so stray spaces in what someone typed
// don't cause a miss. Only the signed-in user's own labels (via
// store.LabelsOf, already owner-scoped) are ever consulted - a label on
// another user's device can never match here, by construction, not by a
// check. Zero matches is reported exactly like "not yours" (pkgError.
// ErrDeviceNotFound), so an unmatched label leaks nothing; two or more of the
// user's own devices sharing that label is reported as the distinct problem
// it is, since silently picking one would be a worse bug than today's dead
// end.
func (r *Resolver) resolveByLabel(ctx context.Context, userID, typed string) (string, error) {
	needle, err := NormalizeLabel(typed)
	if err != nil || needle == "" {
		return "", pkgError.ErrDeviceNotFound
	}
	labels, err := r.st.LabelsOf(ctx, userID)
	if err != nil {
		return "", pkgError.ErrDeviceNotFound
	}
	var matches []string
	for id, label := range labels {
		if label != "" && strings.EqualFold(label, needle) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return "", pkgError.ErrDeviceNotFound
	case 1:
		return matches[0], nil
	default:
		return "", pkgError.ValidationError(fmt.Sprintf(
			"more than one of your WhatsApp numbers is named %q; rename one so the names differ, or pass its device_id", needle))
	}
}

// deviceChoiceMessage is ErrDeviceIDRequired's replacement text for one
// signed-in user, right now: every one of their own devices, named by its
// label when it has one or by its phone number (already in memory on the
// device manager - no extra lookup) when it does not, so the message itself
// tells a person what to say instead of just refusing. ids is already this
// user's own device list (store.DevicesOf); labels is the matching
// store.LabelsOf map, fetched once by the caller.
func (r *Resolver) deviceChoiceMessage(ids []string, labels map[string]string) pkgError.ValidationError {
	names := make([]string, len(ids))
	anyLabelled := false
	for i, id := range ids {
		if label := labels[id]; label != "" {
			names[i] = label
			anyLabelled = true
			continue
		}
		if inst, ok := r.dm.GetDevice(id); ok && inst.PhoneNumber() != "" {
			names[i] = inst.PhoneNumber()
			continue
		}
		names[i] = "an unnamed number"
	}
	list := strings.Join(names, ", ")
	if !anyLabelled {
		return pkgError.ValidationError(fmt.Sprintf(
			"you have several WhatsApp numbers linked (%s) and none is named yet; "+
				"name one with wtf_rename_number, or Rename on /devices, then say that name instead of device_id", list))
	}
	return pkgError.ValidationError(fmt.Sprintf(
		"you have several WhatsApp numbers linked: say which one by name - %s - or pass its device_id", list))
}

func NewDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "d_" + hex.EncodeToString(b)
}
