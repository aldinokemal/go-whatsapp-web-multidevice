package usecase

import (
	"context"
	"encoding/json"
	"testing"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waVnameCert"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

type countingContactStore struct {
	store.NoopStore
	contacts map[types.JID]types.ContactInfo
	reads    int
}

func (s *countingContactStore) GetContact(_ context.Context, jid types.JID) (types.ContactInfo, error) {
	s.reads++
	return s.contacts[jid], nil
}

func TestResolveParticipantDisplayNamePreservesSourceOrdering(t *testing.T) {
	participantJID := types.NewJID("628123456789", types.DefaultUserServer)
	verifiedName := "Verified Business"
	userInfo := map[types.JID]types.UserInfo{
		participantJID: {
			VerifiedName: &types.VerifiedName{
				Details: &waVnameCert.VerifiedNameCertificate_Details{
					VerifiedName: &verifiedName,
				},
			},
		},
	}

	tests := []struct {
		name      string
		seed      string
		contact   types.ContactInfo
		want      string
		wantReads int
	}{
		{
			name:      "seed wins before contact and verified name",
			seed:      "Participant Seed",
			contact:   types.ContactInfo{Found: true, FullName: "Saved Contact"},
			want:      "Participant Seed",
			wantReads: 0,
		},
		{
			name:      "contact wins before verified name",
			contact:   types.ContactInfo{Found: true, FullName: "Saved Contact"},
			want:      "Saved Contact",
			wantReads: 1,
		},
		{
			name:      "verified name follows an unavailable contact",
			want:      "Verified Business",
			wantReads: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contacts := &countingContactStore{
				contacts: map[types.JID]types.ContactInfo{
					participantJID: tt.contact,
				},
			}
			client := &whatsmeow.Client{
				Store: &store.Device{Contacts: contacts},
			}

			got := resolveParticipantDisplayName(
				context.Background(),
				client,
				tt.seed,
				participantJID,
				participantJID,
				userInfo,
			)
			if got != tt.want {
				t.Fatalf("resolveParticipantDisplayName() = %q, want %q", got, tt.want)
			}
			if contacts.reads != tt.wantReads {
				t.Fatalf("contact reads = %d, want %d", contacts.reads, tt.wantReads)
			}
		})
	}
}

func TestGroupParticipantCanonicalPN(t *testing.T) {
	pn := types.NewJID("12025550101", types.DefaultUserServer)
	otherPN := types.NewJID("12025550102", types.DefaultUserServer)
	lid := types.NewJID("555000001", types.HiddenUserServer)
	otherLID := types.NewJID("555000002", types.HiddenUserServer)
	withDevice := func(jid types.JID) types.JID { jid.Device = 1; return jid }
	withAgent := func(jid types.JID) types.JID { jid.RawAgent = 1; return jid }
	withIntegrator := func(jid types.JID) types.JID { jid.Integrator = 1; return jid }

	tests := []struct {
		name  string
		entry types.GroupParticipant
		want  string
	}{
		{"phone JID alone", types.GroupParticipant{JID: pn}, pn.String()},
		{"phone JID with matching phone field", types.GroupParticipant{JID: pn, PhoneNumber: pn}, pn.String()},
		{"LID with phone field", types.GroupParticipant{JID: lid, PhoneNumber: pn}, pn.String()},
		{"LID with matching LID and phone fields", types.GroupParticipant{JID: lid, LID: lid, PhoneNumber: pn}, pn.String()},
		{"empty entry", types.GroupParticipant{}, ""},
		{"unresolved LID is not a phone number", types.GroupParticipant{JID: lid}, ""},
		{"unresolved LID with phone-shaped user", types.GroupParticipant{JID: types.NewJID(pn.User, types.HiddenUserServer)}, ""},
		{"failed entry", types.GroupParticipant{JID: pn, Error: 403}, ""},
		{"failed LID entry", types.GroupParticipant{JID: lid, PhoneNumber: pn, Error: 403}, ""},
		{"pending add request", types.GroupParticipant{JID: lid, PhoneNumber: pn, AddRequest: &types.GroupParticipantAddRequest{}}, ""},
		{"phone field is a LID", types.GroupParticipant{JID: lid, PhoneNumber: lid}, ""},
		{"phone JID conflicts with phone field", types.GroupParticipant{JID: pn, PhoneNumber: otherPN}, ""},
		{"LID field names another LID", types.GroupParticipant{JID: lid, LID: otherLID, PhoneNumber: pn}, ""},
		{"phone field without primary JID", types.GroupParticipant{PhoneNumber: pn}, ""},
		{"legacy c.us server", types.GroupParticipant{JID: types.NewJID(pn.User, types.LegacyUserServer)}, ""},
		{"phone field on unknown server", types.GroupParticipant{JID: lid, PhoneNumber: types.NewJID(pn.User, "example.invalid")}, ""},
		{"phone JID with device", types.GroupParticipant{JID: withDevice(pn)}, ""},
		{"phone JID with agent", types.GroupParticipant{JID: withAgent(pn)}, ""},
		{"phone JID with integrator", types.GroupParticipant{JID: withIntegrator(pn)}, ""},
		{"LID with device", types.GroupParticipant{JID: withDevice(lid), PhoneNumber: pn}, ""},
		{"phone field with device", types.GroupParticipant{JID: lid, PhoneNumber: withDevice(pn)}, ""},
		{"non-digit phone user", types.GroupParticipant{JID: types.NewJID("+12025550101", types.DefaultUserServer)}, ""},
		{"non-digit phone field user", types.GroupParticipant{JID: lid, PhoneNumber: types.NewJID("1202555x0101", types.DefaultUserServer)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := groupParticipantCanonicalPN(tt.entry); got != tt.want {
				t.Errorf("groupParticipantCanonicalPN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGroupParticipantDataCarriesCanonicalPN(t *testing.T) {
	pn := types.NewJID("12025550101", types.DefaultUserServer)
	lid := types.NewJID("555000001", types.HiddenUserServer)
	entry := types.GroupParticipant{JID: lid, LID: lid, PhoneNumber: pn, IsAdmin: true}

	got := groupParticipantData(entry, pn.User, "Alice")
	want := domainGroup.GroupParticipant{
		JID:         lid.String(),
		CanonicalPN: pn.String(),
		PhoneNumber: pn.User,
		LID:         lid.String(),
		DisplayName: "Alice",
		IsAdmin:     true,
	}
	if got != want {
		t.Fatalf("groupParticipantData() = %+v, want %+v", got, want)
	}
}

func TestGroupParticipantCanonicalPNIsOmittedWhenUnresolved(t *testing.T) {
	lid := types.NewJID("555000001", types.HiddenUserServer)
	participant := groupParticipantData(types.GroupParticipant{JID: lid, LID: lid}, lid.User, "")
	encoded, err := json.Marshal(participant)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["canonical_pn"]; ok {
		t.Fatalf("canonical_pn must be omitted when unresolved, got %s", encoded)
	}
	for _, key := range []string{"jid", "phone_number", "lid", "is_admin", "is_super_admin"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("existing field %q missing from %s", key, encoded)
		}
	}
}
