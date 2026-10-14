package owners

import (
	"context"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
)

type PairedDevice struct{ ID, JID string }

type PairSource interface {
	Paired() []PairedDevice
	Logout(ctx context.Context, deviceID string) error
}

// GuardDuplicatePhone runs after a device pairs. If another user's device is
// already linked to the same WhatsApp number, the new link is logged out.
func GuardDuplicatePhone(ctx context.Context, src PairSource, st *store.Store, deviceID string) (bool, error) {
	newOwner, err := st.OwnerOf(ctx, deviceID)
	if err != nil {
		return false, nil // not a WTF-owned device; nothing to guard
	}
	var number string
	for _, d := range src.Paired() {
		if d.ID == deviceID {
			number = userPart(d.JID)
		}
	}
	if number == "" {
		return false, nil
	}
	for _, d := range src.Paired() {
		if d.ID == deviceID || userPart(d.JID) != number {
			continue
		}
		if owner, err := st.OwnerOf(ctx, d.ID); err == nil && owner != newOwner {
			_ = st.Audit(ctx, store.AuditEvent{UserID: newOwner, Action: "device_link_refused_duplicate", DeviceID: deviceID})
			return true, src.Logout(ctx, deviceID)
		}
	}
	return false, nil
}

// userPart turns "27825550999:12@s.whatsapp.net" into "27825550999".
func userPart(jid string) string {
	u := jid
	if i := strings.IndexByte(u, '@'); i >= 0 {
		u = u[:i]
	}
	if i := strings.IndexByte(u, ':'); i >= 0 {
		u = u[:i]
	}
	return u
}
