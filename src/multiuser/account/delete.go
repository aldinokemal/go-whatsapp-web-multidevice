// Package account deletes a WTF account and everything tied to it.
//
// Deletion is irreversible and resumable. The first thing it changes is the
// user's status, to "deleting": from then on every sign-in check refuses the
// account (sessions, API keys, passkeys, sign-in links, OAuth), and no admin
// can set it back (store.ErrDeleting). Every step after that is idempotent, so
// a deletion that stops half way (a crash, a WhatsApp error) is finished by
// the next Delete call or by ResumePending (at start-up, then hourly).
package account

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/sirupsen/logrus"
)

// ErrOwnsServiceNumber refuses deleting the account that owns the service
// (sign-in) number: deleting it would stop sign-in for everyone. Another
// number must be picked first.
var ErrOwnsServiceNumber = errors.New("account owns the service number")

// Deleter deletes accounts. Store and Devices are required; the rest are
// optional and skipped when nil.
type Deleter struct {
	Store *store.Store

	// Devices is the inner (unscoped) device usecase. Its RemoveDevice runs
	// DeviceManager.PurgeDevice: a best-effort WhatsApp logout, then the
	// whatsmeow session and keys, the device's chat storage partitions and
	// Chatwoot config, and the registry slot. ErrDeviceNotFound counts as
	// done. The owner-scoped ScopedDevices is not used: a deleting user is
	// already refused by it.
	Devices interface {
		RemoveDevice(ctx context.Context, deviceID string) error
	}

	// Chats clears the slot's own chat storage partition even when the device
	// manager no longer knows the slot (a ghost), so nothing is left for the
	// retention window to catch. It is chatstorage.IChatStorageRepository's
	// DeleteDeviceData; idempotent.
	Chats interface {
		DeleteDeviceData(deviceID string) error
	}

	// Grants revokes every OAuth grant (authorisation codes, access and
	// refresh tokens) issued to the user, by subject = user id. Setup wires
	// the MCP OAuth server here when MCP is on; nil when it is off (there
	// are no grants then), and the "deleting" status refuses any token
	// regardless (Active check).
	Grants interface {
		RevokeSubject(ctx context.Context, subject string) error
	}

	// MediaRoot is where downloaded media lives, one folder per device
	// ("statics/media/<device id>", see multiuser/files). Empty skips the
	// file step.
	MediaRoot string

	// ServiceDevice returns the device id of the service (sign-in) number,
	// or "" when none is set.
	ServiceDevice func(ctx context.Context) string

	// mu runs one deletion at a time, so a start-up resume and the person's
	// own click never interleave steps.
	mu sync.Mutex
}

// Delete deletes the account userID and everything tied to it. Deleting an
// account that no longer exists is not an error. See the package comment for
// how an interrupted deletion finishes.
func (d *Deleter) Delete(ctx context.Context, userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Once started, finish even if the request that asked goes away.
	ctx = context.WithoutCancel(ctx)

	u, err := d.Store.UserByID(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil // already deleted
	}
	if err != nil {
		return err
	}
	owned, err := d.Store.DevicesOf(ctx, u.ID)
	if err != nil {
		return err
	}

	// 1. Refuse, changing nothing, if this account owns the service number.
	if d.ServiceDevice != nil {
		if svc := d.ServiceDevice(ctx); svc != "" {
			for _, id := range owned {
				if id == svc {
					return ErrOwnsServiceNumber
				}
			}
		}
	}

	// 2. Mark the account deleting. From here nothing signs in as it.
	if err := d.Store.SetUserStatus(ctx, u.ID, store.StatusDeleting); err != nil {
		return fmt.Errorf("mark deleting: %w", err)
	}

	// 3. Credentials: sessions, API keys, sign-in links, OAuth grants.
	if err := d.Store.DeleteUserSessions(ctx, u.ID); err != nil {
		return fmt.Errorf("sessions: %w", err)
	}
	if err := d.Store.RevokeAllAPIKeys(ctx, u.ID); err != nil {
		return fmt.Errorf("api keys: %w", err)
	}
	if err := d.Store.DeleteLoginTokensFor(ctx, u.Phone); err != nil {
		return fmt.Errorf("sign-in links: %w", err)
	}
	if d.Grants != nil {
		if err := d.Grants.RevokeSubject(ctx, u.ID); err != nil {
			return fmt.Errorf("oauth grants: %w", err)
		}
	}

	// 4. Each linked number: unlink and purge, clear its chat partition and
	// media, then drop the owner row (last, so a retry finds it again).
	for _, id := range owned {
		if err := d.Devices.RemoveDevice(ctx, id); err != nil && !errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fmt.Errorf("remove device %s: %w", id, err)
		}
		if d.Chats != nil {
			if err := d.Chats.DeleteDeviceData(id); err != nil {
				return fmt.Errorf("chat data for %s: %w", id, err)
			}
		}
		if err := d.removeMedia(id); err != nil {
			return fmt.Errorf("media for %s: %w", id, err)
		}
		if err := d.Store.RemoveOwner(ctx, id); err != nil {
			return fmt.Errorf("owner row %s: %w", id, err)
		}
	}

	// 5. The person's waitlist row, if they came in that way.
	if err := d.Store.RemoveFromWaitlist(ctx, u.Phone); err != nil {
		return fmt.Errorf("waitlist row: %w", err)
	}

	// 6. One transaction: scrub their audit detail, write account_deleted
	// (user id only), delete the user row. Cascades take passkeys and
	// anything else still hanging off it.
	if err := d.Store.EraseUser(ctx, u.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("erase user: %w", err)
	}
	logrus.Infof("[WTF] account %s deleted (%d numbers)", u.ID, len(owned))
	return nil
}

// ResumePending finishes every account left in "deleting" (the runtime calls
// it at start-up and then hourly). It
// carries on past a failure and returns them all joined.
func (d *Deleter) ResumePending(ctx context.Context) error {
	users, err := d.Store.UsersWithStatus(ctx, store.StatusDeleting)
	if err != nil {
		return err
	}
	var errs error
	for _, u := range users {
		if err := d.Delete(ctx, u.ID); err != nil {
			logrus.WithError(err).Warnf("[WTF] could not finish deleting account %s", u.ID)
			errs = errors.Join(errs, fmt.Errorf("account %s: %w", u.ID, err))
		}
	}
	return errs
}

// removeMedia removes <MediaRoot>/<deviceID>, only when that path is a
// direct child of the media root. A symlink there is unlinked, not followed.
// A missing folder is done already.
func (d *Deleter) removeMedia(deviceID string) error {
	if d.MediaRoot == "" {
		return nil
	}
	root := filepath.Clean(d.MediaRoot)
	dir := filepath.Join(root, deviceID)
	if deviceID == "" || deviceID == "." || deviceID == ".." ||
		strings.ContainsAny(deviceID, `/\`) || filepath.Dir(dir) != root || filepath.Base(dir) != deviceID {
		logrus.Warnf("[WTF] account deletion: refusing media path for device id %q (not directly under the media root)", deviceID)
		return nil
	}
	return os.RemoveAll(dir)
}
