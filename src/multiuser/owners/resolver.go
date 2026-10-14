// Package owners enforces that a user only ever reaches WhatsApp accounts they own.
package owners

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

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
			return nil, "", ErrDeviceIDRequired
		}
	} else {
		owner, err := r.st.OwnerOf(ctx, deviceID)
		if err != nil || owner != u.ID {
			return nil, deviceID, pkgError.ErrDeviceNotFound
		}
	}
	inst, ok := r.dm.GetDevice(deviceID)
	if !ok {
		return nil, deviceID, pkgError.ErrDeviceNotFound
	}
	return inst, deviceID, nil
}

func NewDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "d_" + hex.EncodeToString(b)
}
