package web

import (
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
)

// Number states, derived at render time from the live device and what
// accounts.db remembers about its slot (house rule T2: never stored).
const (
	numConnected    = "connected"    // linked and online
	numDisconnected = "disconnected" // linked, not online right now
	numSignedOut    = "signed_out"   // the phone signed this slot out
	numRefused      = "refused"      // the last link was refused
	numNotLinked    = "not_linked"   // never finished linking
)

// numberView is one WhatsApp number as a row shows it, for the owner's own
// list and the admin's view of a user's numbers.
type numberView struct {
	ID      string
	State   string
	Phone   string // bare digits or "+..." as stored; shown with the phone helper
	Name    string
	Added   time.Time
	Refusal string // store.Refusal* when State is numRefused
}

// Title is the row's main line: the name, else the number, else nothing.
func (n numberView) Title() string {
	if n.Name != "" {
		return n.Name
	}
	return displayPhone(n.Phone)
}

// Linked reports whether the number is linked right now (connected or not).
func (n numberView) Linked() bool { return n.State == numConnected || n.State == numDisconnected }

// RefusalText is what the row says when the last link was refused.
func (n numberView) RefusalText() string {
	if n.Refusal == store.RefusalDuplicate {
		return "That number is already linked to another account."
	}
	return "We couldn't confirm that number. Link it again."
}

func numberViews(devs []domainDevice.Device, slots map[string]store.Slot) []numberView {
	out := make([]numberView, 0, len(devs))
	for _, d := range devs {
		sl := slots[d.ID]
		v := numberView{ID: d.ID, Phone: d.PhoneNumber, Name: d.DisplayName, Added: d.CreatedAt}
		switch {
		case d.PhoneNumber != "" && d.State == domainDevice.DeviceStateLoggedIn:
			v.State = numConnected
		case d.PhoneNumber != "":
			v.State = numDisconnected
		case sl.Refusal != "":
			v.State, v.Refusal = numRefused, sl.Refusal
			v.Phone, v.Name = "", ""
		case sl.SignedOutAt != nil && (sl.LastPhone != "" || sl.LastName != ""):
			v.State, v.Phone, v.Name = numSignedOut, sl.LastPhone, sl.LastName
		default:
			v.State = numNotLinked
		}
		out = append(out, v)
	}
	return out
}

// numberMap is numberViews keyed by device id, for templates that range over
// the devices themselves.
func numberMap(devs []domainDevice.Device, slots map[string]store.Slot) map[string]numberView {
	m := make(map[string]numberView, len(devs))
	for _, v := range numberViews(devs, slots) {
		m[v.ID] = v
	}
	return m
}
