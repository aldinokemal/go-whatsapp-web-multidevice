package usecase

import (
	"context"
	"testing"

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

func TestGroupParticipantPhoneUserIsEmptyWhenOnlyTheLIDIsKnown(t *testing.T) {
	lid := types.NewJID("84198740205822", types.HiddenUserServer)
	pn := types.NewJID("628123456789", types.DefaultUserServer)

	tests := []struct {
		name string
		p    types.GroupParticipant
		want string
	}{
		{"phone-number wire JID", types.GroupParticipant{JID: pn, LID: lid}, "628123456789"},
		{"LID wire JID with phone number from the group response", types.GroupParticipant{JID: lid, PhoneNumber: pn}, "628123456789"},
		{"LID wire JID without any phone number", types.GroupParticipant{JID: lid, LID: lid}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := groupParticipantPhoneUser(context.Background(), nil, tt.p); got != tt.want {
				t.Fatalf("phone = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWireParticipantLookupAndPhoneIsEmptyWhenOnlyTheLIDIsKnown(t *testing.T) {
	lid := types.NewJID("84198740205822", types.HiddenUserServer)
	pn := types.NewJID("628123456789", types.DefaultUserServer)

	lookup, phone := wireParticipantLookupAndPhone(context.Background(), nil, pn)
	if lookup != pn || phone != "628123456789" {
		t.Fatalf("phone-number wire JID: lookup=%s phone=%q", lookup, phone)
	}
	lookup, phone = wireParticipantLookupAndPhone(context.Background(), nil, lid)
	if lookup != lid || phone != "" {
		t.Fatalf("unresolved LID: lookup=%s phone=%q, want lookup=%s phone=\"\"", lookup, phone, lid)
	}
}
