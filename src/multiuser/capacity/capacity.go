// Package capacity answers "how full is the server": numbers linked against
// the WTF_MAX_PHONES cap, users, and people waiting.
package capacity

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/sirupsen/logrus"
)

// Capacity is one reading. Max 0 means no cap. Linked counts every owned
// slot, including one whose QR code was never scanned: it holds a
// reservation.
type Capacity struct {
	Linked  int `json:"linked"`
	Max     int `json:"max"`
	Users   int `json:"users"`
	Waiting int `json:"waiting"`
}

type Service struct {
	Store     *store.Store
	MaxPhones int
}

func (s Service) Snapshot(ctx context.Context) (Capacity, error) {
	c := Capacity{Max: s.MaxPhones}
	var err error
	if c.Linked, err = s.Store.OwnedDeviceCount(ctx); err != nil {
		return Capacity{}, err
	}
	if c.Users, _, err = s.Store.ListUsersPage(ctx, store.UsersQuery{Limit: 1}); err != nil {
		return Capacity{}, err
	}
	if c.Waiting, err = s.Store.WaitlistCount(ctx, store.WaitlistWaiting); err != nil {
		return Capacity{}, err
	}
	return c, nil
}

// Full reports whether the cap is reached. It fails closed: when the count
// cannot be read it says full, so sign-up sends people to the waitlist
// rather than creating accounts that could link nothing. Linking itself is
// guarded separately and atomically by store.ReserveOwner.
func (s Service) Full(ctx context.Context) bool {
	if s.MaxPhones <= 0 {
		return false
	}
	n, err := s.Store.OwnedDeviceCount(ctx)
	if err != nil {
		logrus.WithError(err).Warn("[WTF] capacity: could not count linked numbers; treating the server as full")
		return true
	}
	return n >= s.MaxPhones
}
