package owners

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePairs struct {
	devices   []PairedDevice
	loggedOut []string
}

func (f *fakePairs) Paired() []PairedDevice { return f.devices }
func (f *fakePairs) Logout(_ context.Context, id string) error {
	f.loggedOut = append(f.loggedOut, id)
	return nil
}

func TestSecondUserCannotLinkTheSameNumber(t *testing.T) {
	st, _, _, _ := fixture(t) // d_a1 is a's, d_b1 is b's
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550999@s.whatsapp.net"}, // b just scanned with a's phone
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.NoError(t, err)
	require.True(t, refused)
	require.Equal(t, []string{"d_b1"}, src.loggedOut, "the newcomer is logged out; the first owner keeps it")
}

func TestSameUserRelinkIsAllowed(t *testing.T) {
	st, _, _, _ := fixture(t) // d_a1 and d_a2 are both a's
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_a2", JID: "27825550999@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_a2")
	require.NoError(t, err)
	require.False(t, refused)
	require.Empty(t, src.loggedOut)
}
