package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

type pnLIDMapperStub struct {
	lids map[types.JID]types.JID
	err  error
}

func (s *pnLIDMapperStub) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	if s.err != nil {
		return types.EmptyJID, s.err
	}
	return s.lids[pn.ToNonAD()], nil
}

type failingContactLister struct{}

func (failingContactLister) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	return nil, errors.New("contact store unavailable")
}

func TestBuildDetailedContactsKeepsEveryNameSeparate(t *testing.T) {
	pn := types.NewJID("628123456789", types.DefaultUserServer)
	pnDevice := types.JID{User: "628123456788", Server: types.DefaultUserServer, Device: 3}
	lidOfPN := types.NewJID("100000000001", types.HiddenUserServer)
	lidOnly := types.NewJID("100000000002", types.HiddenUserServer)
	group := types.NewJID("120363000000000001", types.GroupServer)
	newsletter := types.NewJID("120363000000000002", types.NewsletterServer)

	contacts := &chatContactStoreStub{contacts: map[types.JID]types.ContactInfo{
		pn:         {Found: true, FullName: "Alice Example", FirstName: "Alice", PushName: "Ali", BusinessName: "Alice Shop"},
		pnDevice:   {Found: true, PushName: "Bob"},
		lidOnly:    {Found: true, PushName: "Carol"},
		group:      {Found: true, FullName: "Some Group"},
		newsletter: {Found: true, FullName: "Some Channel"},
	}}

	got, err := buildDetailedContacts(context.Background(), contacts, &pnLIDMapperStub{lids: map[types.JID]types.JID{pn: lidOfPN}})
	require.NoError(t, err)
	assert.Equal(t, []domainUser.MyListContactsDetailedData{
		{JID: lidOnly.String(), PushName: "Carol"},
		{JID: pnDevice.ToNonAD().String(), PushName: "Bob"},
		{JID: pn.String(), LID: lidOfPN.String(), FullName: "Alice Example", FirstName: "Alice", PushName: "Ali", BusinessName: "Alice Shop"},
	}, got)
}

func TestBuildDetailedContactsDropsBlankNames(t *testing.T) {
	pn := types.NewJID("628123456789", types.DefaultUserServer)
	got, err := buildDetailedContacts(context.Background(),
		&chatContactStoreStub{contacts: map[types.JID]types.ContactInfo{pn: {FullName: "   ", PushName: "\t", FirstName: " Al "}}},
		&pnLIDMapperStub{})
	require.NoError(t, err)
	assert.Equal(t, []domainUser.MyListContactsDetailedData{{JID: pn.String(), FirstName: " Al "}}, got)
}

func TestBuildDetailedContactsEmptyStoreReturnsEmptySlice(t *testing.T) {
	got, err := buildDetailedContacts(context.Background(), &chatContactStoreStub{}, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got)

	raw, err := json.Marshal(domainUser.MyListContactsDetailedResponse{Data: got})
	require.NoError(t, err)
	assert.JSONEq(t, `{"data":[]}`, string(raw))
}

func TestBuildDetailedContactsLIDLookupFailureOmitsOnlyTheLID(t *testing.T) {
	pn := types.NewJID("628123456789", types.DefaultUserServer)
	got, err := buildDetailedContacts(context.Background(),
		&chatContactStoreStub{contacts: map[types.JID]types.ContactInfo{pn: {PushName: "Ali"}}},
		&pnLIDMapperStub{err: errors.New("lid store unavailable")})
	require.NoError(t, err)
	assert.Equal(t, []domainUser.MyListContactsDetailedData{{JID: pn.String(), PushName: "Ali"}}, got)
}

func TestBuildDetailedContactsPropagatesContactStoreError(t *testing.T) {
	_, err := buildDetailedContacts(context.Background(), failingContactLister{}, nil)
	assert.Error(t, err, "a contact store failure must not look like an empty contact list")
}

func TestMyListContactsDetailedJSONOmitsEmptyOptionalFields(t *testing.T) {
	raw, err := json.Marshal(domainUser.MyListContactsDetailedResponse{
		Data: []domainUser.MyListContactsDetailedData{{JID: "628123456789@s.whatsapp.net", PushName: "Ali"}},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"data":[{"jid":"628123456789@s.whatsapp.net","push_name":"Ali"}]}`, string(raw))
}

func TestMyListContactsDetailedRequiresTheDeviceClient(t *testing.T) {
	ctx := whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance("device-a", nil, nil))
	_, err := serviceUser{}.MyListContactsDetailed(ctx)
	assert.ErrorIs(t, err, pkgError.ErrWaCLI)
}
