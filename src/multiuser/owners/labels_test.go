package owners

import (
	"context"
	"strings"
	"testing"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

// Behaviour: what a name may be.
func TestNormalizeLabel(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		ok             bool
	}{
		{"trims and collapses spaces", "  Work   phone ", "Work phone", true},
		{"collapses tabs and newlines", "Work\t\n phone", "Work phone", true},
		{"empty clears", "", "", true},
		{"only spaces clears", "   ", "", true},
		{"40 runes is fine", strings.Repeat("é", 40), strings.Repeat("é", 40), true},
		{"41 runes is too long", strings.Repeat("a", 41), "", false},
		{"emoji with joiners is fine", "Mom 👩‍👧", "Mom 👩‍👧", true},
		{"html is just text (escaped when shown)", "<script>x</script>", "<script>x</script>", true},
		{"right-to-left override", "‮enohp", "", false},
		{"left-to-right embedding", "a‪b", "", false},
		{"pop directional formatting", "a‬b", "", false},
		{"first strong isolate", "a⁨b", "", false},
		{"pop directional isolate", "a⁩b", "", false},
		{"control character", "a\x07b", "", false},
		{"NUL", "a\x00b", "", false},
		{"DEL", "a\x7fb", "", false},
		{"invalid UTF-8", "a\xffb", "", false},
		{"Arabic text is fine", "هاتف العمل", "هاتف العمل", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeLabel(tc.in)
			if !tc.ok {
				require.Error(t, err)
				var v pkgError.ValidationError
				require.ErrorAs(t, err, &v, "a bad name is a validation error (400), not a server error")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// Behaviour: the list and the single device carry the owner's own names.
func TestListShowsMyLabels(t *testing.T) {
	st, _, a, b := fixture(t)
	ctx := context.Background()
	require.NoError(t, st.SetLabel(ctx, a.ID, "d_a1", "Work phone"))
	require.NoError(t, st.SetLabel(ctx, b.ID, "d_b1", "Bob's"))
	inner := &innerStub{all: []domainDevice.Device{{ID: "d_a1"}, {ID: "d_a2"}, {ID: "d_b1"}}}
	svc := NewScopedDevices(inner, st)

	list, err := svc.ListDevices(auth.WithUser(ctx, a))
	require.NoError(t, err)
	got := map[string]string{}
	for _, d := range list {
		got[d.ID] = d.Label
	}
	require.Equal(t, map[string]string{"d_a1": "Work phone", "d_a2": ""}, got)

	one, err := svc.GetDevice(auth.WithUser(ctx, a), "d_a1")
	require.NoError(t, err)
	require.Equal(t, "Work phone", one.Label)
}

// Behaviour + security: naming is owner-checked; another user's number is the
// same not-found as a number that does not exist, and is left unnamed. The
// audit line never carries the name itself.
func TestLabelAnotherUsersDeviceIsNotFound(t *testing.T) {
	st, _, a, b := fixture(t)
	ctxA := auth.WithUser(context.Background(), a)

	_, err := SetDeviceLabel(ctxA, st, "d_b1", "mine now")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound)
	_, err = SetDeviceLabel(ctxA, st, "d_nope", "x")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound)
	_, err = SetDeviceLabel(context.Background(), st, "d_a1", "x")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound, "no user, no naming")
	labels, err := st.LabelsOf(context.Background(), b.ID)
	require.NoError(t, err)
	require.Equal(t, "", labels["d_b1"])

	got, err := SetDeviceLabel(ctxA, st, "d_a1", "  Work   phone ")
	require.NoError(t, err)
	require.Equal(t, "Work phone", got)
	got, err = SetDeviceLabel(ctxA, st, "d_a1", "")
	require.NoError(t, err)
	require.Equal(t, "", got)
	_, err = SetDeviceLabel(ctxA, st, "d_a1", "‮enohp")
	require.Error(t, err)

	rows, _, err := st.ListAudit(context.Background(), store.AuditQuery{UserID: a.ID, Limit: 50})
	require.NoError(t, err)
	renamed := 0
	for _, r := range rows {
		if r.Action == "device_renamed" {
			renamed++
			require.Equal(t, "d_a1", r.DeviceID)
			require.NotContains(t, r.Detail, "Work")
		}
	}
	require.Equal(t, 2, renamed, "one line per successful change, none for refusals")
}
