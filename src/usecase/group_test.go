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
