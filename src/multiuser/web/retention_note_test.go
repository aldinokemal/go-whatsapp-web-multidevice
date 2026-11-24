package web

import (
	"testing"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/stretchr/testify/require"
)

// Behaviour: My WhatsApp says how long messages and media are kept, from the
// setting, and says nothing while retention is off.
func TestHomeSaysHowLongMessagesAreKept(t *testing.T) {
	for _, tc := range []struct {
		hours int
		want  string
	}{
		{24, "Messages and media are deleted after 24 hours."},
		{1, "Messages and media are deleted after 1 hour."},
		{36, "Messages and media are deleted after 36 hours."},
		{72, "Messages and media are deleted after 3 days."},
	} {
		for _, devices := range [][]domainDevice.Device{nil, {{ID: "d_a", PhoneNumber: "27825550101", State: domainDevice.DeviceStateLoggedIn}}} {
			stub := &devStub{devices: devices}
			e := withDevices(t, stub)
			ck, uid := signedIn(t, e, "+27825550101")
			for _, d := range devices {
				require.NoError(t, e.st.SetOwner(t.Context(), d.ID, uid))
			}
			e.d.RetentionHours = tc.hours
			remount(e, stub)
			require.Contains(t, getPage(t, e, ck, "/"), tc.want, "hours %d, %d devices", tc.hours, len(devices))
		}
	}
}

func TestHomeSaysNothingAboutRetentionWhenOff(t *testing.T) {
	stub := &devStub{}
	e := withDevices(t, stub)
	ck, _ := signedIn(t, e, "+27825550101")
	e.d.RetentionHours = 0
	remount(e, stub)
	require.NotContains(t, getPage(t, e, ck, "/"), "are deleted after")
}
