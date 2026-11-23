package owners

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

// Gap B: a refused duplicate is recorded against the slot, not only in the audit log.
func TestARefusedDuplicateIsRecordedOnTheSlot(t *testing.T) {
	st, _, _, b := fixture(t)
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550999:12@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.NoError(t, err)
	require.True(t, refused)
	slots, err := st.SlotsOf(context.Background(), b.ID)
	require.NoError(t, err)
	require.Equal(t, store.RefusalDuplicate, slots["d_b1"].Refusal)
}

func TestAnUnreadableNumberIsRecordedAsUnreadable(t *testing.T) {
	st, _, _, b := fixture(t)
	src := &fakePairs{devices: []PairedDevice{{ID: "d_b1", JID: "123@lid"}}}
	refused, _ := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.True(t, refused)
	slots, _ := st.SlotsOf(context.Background(), b.ID)
	require.Equal(t, store.RefusalUnreadable, slots["d_b1"].Refusal)
}

// Gap C: an admin unlinks a user's number through the same owner-checked
// removal, and the audit names both the user and the admin.
func TestAdminRemovalIsOwnerCheckedAndAudited(t *testing.T) {
	st, _, a, b := fixture(t)
	admin, _ := st.CreateUser(context.Background(), "+15555550100")
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)

	// Naming a device that is not the target's is refused like a missing one.
	err := RemoveDeviceAsAdmin(context.Background(), svc, admin, a, "d_b1")
	require.ErrorIs(t, err, pkgError.ErrDeviceNotFound)
	require.Empty(t, inner.removed)

	require.NoError(t, RemoveDeviceAsAdmin(context.Background(), svc, admin, b, "d_b1"))
	require.Equal(t, []string{"d_b1"}, inner.removed)
	_, err = st.OwnerOf(context.Background(), "d_b1")
	require.ErrorIs(t, err, store.ErrNotFound)

	rows, _, err := st.ListAudit(context.Background(), store.AuditQuery{UserID: b.ID})
	require.NoError(t, err)
	require.Equal(t, "device_remove", rows[0].Action)
	require.Equal(t, admin.ID, rows[0].ActorID)
}

func TestRemoveAllDevicesAsAdminUnlinksEveryNumber(t *testing.T) {
	st, _, a, _ := fixture(t)
	admin, _ := st.CreateUser(context.Background(), "+15555550100")
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)
	n, err := RemoveAllDevicesAsAdmin(context.Background(), svc, st, admin, a)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.ElementsMatch(t, []string{"d_a1", "d_a2"}, inner.removed)
	ids, _ := st.DevicesOf(context.Background(), a.ID)
	require.Empty(t, ids)
	_, ok := auth.ActorFrom(context.Background())
	require.False(t, ok)
}
