package utils

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

type fakeWhatsappLookup struct {
	phones        []string
	numberResults []types.IsOnWhatsAppResponse
	numberErr     error
	userInfo      map[types.JID]types.UserInfo
	userInfoCalls int
}

func (f *fakeWhatsappLookup) IsOnWhatsApp(_ context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error) {
	f.phones = phones
	return f.numberResults, f.numberErr
}

func (f *fakeWhatsappLookup) GetUserInfo(_ context.Context, _ []types.JID) (map[types.JID]types.UserInfo, error) {
	f.userInfoCalls++
	return f.userInfo, nil
}

func TestResolveOnWhatsapp(t *testing.T) {
	typed := types.NewJID("5573912345678", types.DefaultUserServer)
	registered := types.NewJID("557312345678", types.DefaultUserServer)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)

	for _, tc := range []struct {
		name         string
		jid          types.JID
		lookup       *fakeWhatsappLookup
		want         types.JID
		wantOn       bool
		wantErr      bool
		wantFallback bool
	}{
		{
			// Brazil's ninth digit: WhatsApp accepts both spellings but registered one.
			name:   "uses the spelling WhatsApp registered",
			jid:    typed,
			lookup: &fakeWhatsappLookup{numberResults: []types.IsOnWhatsAppResponse{{IsIn: true, JID: lid, PhoneNumber: registered}}},
			want:   registered, wantOn: true,
		},
		{
			// Some valid numbers (short Indonesian ones) are missed by the phone-number
			// lookup but found by the JID lookup that sending uses.
			name:   "falls back to the JID lookup sending uses",
			jid:    typed,
			lookup: &fakeWhatsappLookup{userInfo: map[types.JID]types.UserInfo{typed: {LID: lid}}},
			want:   typed, wantOn: true, wantFallback: true,
		},
		{
			name:   "not on WhatsApp",
			jid:    typed,
			lookup: &fakeWhatsappLookup{numberResults: []types.IsOnWhatsAppResponse{{IsIn: false}}, userInfo: map[types.JID]types.UserInfo{typed: {}}},
			want:   typed, wantOn: false, wantFallback: true,
		},
		{
			name:    "a failed lookup is an error, not a negative",
			jid:     typed,
			lookup:  &fakeWhatsappLookup{numberErr: errors.New("timed out")},
			wantErr: true,
		},
		{
			name:   "groups are not looked up",
			jid:    types.NewJID("120363000000000000", types.GroupServer),
			lookup: &fakeWhatsappLookup{numberErr: errors.New("must not be called")},
			want:   types.NewJID("120363000000000000", types.GroupServer), wantOn: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, on, err := ResolveOnWhatsapp(tc.lookup, tc.jid)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveOnWhatsapp() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveOnWhatsapp() error = %v", err)
			}
			if got != tc.want || on != tc.wantOn {
				t.Errorf("ResolveOnWhatsapp() = %v, %v; want %v, %v", got, on, tc.want, tc.wantOn)
			}
			if fellBack := tc.lookup.userInfoCalls > 0; fellBack != tc.wantFallback {
				t.Errorf("JID lookup called = %v, want %v", fellBack, tc.wantFallback)
			}
			if tc.jid.Server == types.DefaultUserServer && (len(tc.lookup.phones) != 1 || tc.lookup.phones[0] != "+"+tc.jid.User) {
				t.Errorf("phone-number lookup got %q, want [+%s]", tc.lookup.phones, tc.jid.User)
			}
		})
	}
}
