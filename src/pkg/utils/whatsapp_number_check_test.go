package utils

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

type fakeWhatsappLookup struct {
	phones        []string
	numberResults []types.IsOnWhatsAppResponse
	numberErr     error
	userInfo      map[types.JID]types.UserInfo
	userInfoErr   error
	userInfoJIDs  []types.JID
}

func (f *fakeWhatsappLookup) IsOnWhatsApp(_ context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error) {
	f.phones = phones
	return f.numberResults, f.numberErr
}

func (f *fakeWhatsappLookup) GetUserInfo(_ context.Context, jids []types.JID) (map[types.JID]types.UserInfo, error) {
	f.userInfoJIDs = jids
	return f.userInfo, f.userInfoErr
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
			// A match without a phone JID says nothing about the typed spelling.
			name:   "a match without a phone JID is checked like a send",
			jid:    typed,
			lookup: &fakeWhatsappLookup{numberResults: []types.IsOnWhatsAppResponse{{IsIn: true, JID: lid}}, userInfo: map[types.JID]types.UserInfo{typed: {}}},
			want:   typed, wantOn: false, wantFallback: true,
		},
		{
			name:   "not on WhatsApp",
			jid:    typed,
			lookup: &fakeWhatsappLookup{numberResults: []types.IsOnWhatsAppResponse{{IsIn: false}}, userInfo: map[types.JID]types.UserInfo{typed: {}}},
			want:   typed, wantOn: false, wantFallback: true,
		},
		{
			// Group participants arrive as typed, so an E.164 "+" can reach the JID.
			name:   "an E.164 plus is not doubled",
			jid:    types.NewJID("+5573912345678", types.DefaultUserServer),
			lookup: &fakeWhatsappLookup{userInfo: map[types.JID]types.UserInfo{typed: {LID: lid}}},
			want:   typed, wantOn: true, wantFallback: true,
		},
		{
			name:    "a failed lookup is an error, not a negative",
			jid:     typed,
			lookup:  &fakeWhatsappLookup{numberErr: errors.New("timed out")},
			wantErr: true,
		},
		{
			name:    "a failed JID lookup is an error, not a negative",
			jid:     typed,
			lookup:  &fakeWhatsappLookup{userInfoErr: errors.New("timed out")},
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
			if tc.wantFallback && (len(tc.lookup.userInfoJIDs) != 1 || tc.lookup.userInfoJIDs[0] != tc.want) {
				t.Errorf("JID lookup got %v, want [%v]", tc.lookup.userInfoJIDs, tc.want)
			} else if !tc.wantFallback && tc.lookup.userInfoJIDs != nil {
				t.Errorf("JID lookup got %v, want no call", tc.lookup.userInfoJIDs)
			}
			wantPhone := "+" + strings.TrimPrefix(tc.jid.User, "+")
			if tc.jid.Server != types.DefaultUserServer {
				if tc.lookup.phones != nil {
					t.Errorf("phone-number lookup got %q, want no call", tc.lookup.phones)
				}
			} else if len(tc.lookup.phones) != 1 || tc.lookup.phones[0] != wantPhone {
				t.Errorf("phone-number lookup got %q, want [%s]", tc.lookup.phones, wantPhone)
			}
		})
	}
}
