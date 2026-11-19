package utils

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

func TestCanonicalUserJID(t *testing.T) {
	pn := types.NewJID("5500912345678", types.DefaultUserServer)
	other := types.NewJID("550012345678", types.DefaultUserServer)
	lid := types.NewJID("42", types.HiddenUserServer)
	ad := pn
	ad.Device = 2
	for _, tc := range []struct {
		name    string
		results []types.IsOnWhatsAppResponse
		want    types.JID
		ok      bool
	}{
		{"prefer phone over jid and strip device", []types.IsOnWhatsAppResponse{{IsIn: true, PhoneNumber: ad, JID: other}}, pn, true},
		{"phone alongside lid", []types.IsOnWhatsAppResponse{{IsIn: true, PhoneNumber: pn, JID: lid}}, pn, true},
		{"fallback phone jid", []types.IsOnWhatsAppResponse{{IsIn: true, JID: other}}, other, true},
		{"lid only", []types.IsOnWhatsAppResponse{{IsIn: true, JID: lid}}, types.JID{}, true},
		{"not registered", []types.IsOnWhatsAppResponse{{IsIn: true, JID: other}, {IsIn: false}}, types.JID{}, false},
		{"empty response", nil, types.JID{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := canonicalUserJID(tc.results)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestUserJIDCache(t *testing.T) {
	var cache userJIDCache
	now := time.Unix(0, 0)
	clock := func() time.Time { return now }
	client, otherClient := &whatsmeow.Client{}, &whatsmeow.Client{}
	pn := types.NewJID("550012345678", types.DefaultUserServer)
	calls := 0
	lookup := func(_ context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error) {
		calls++
		require.Equal(t, []string{"+5500912345678"}, phones)
		return []types.IsOnWhatsAppResponse{{IsIn: true, PhoneNumber: pn}}, nil
	}
	for _, c := range []*whatsmeow.Client{client, client, otherClient} {
		got, ok := cache.resolve(c, "5500912345678", clock, lookup)
		require.True(t, ok)
		require.Equal(t, pn, got)
	}
	require.Equal(t, 2, calls)
	now = now.Add(userJIDCacheTTL)
	_, ok := cache.resolve(client, "+5500912345678", clock, lookup)
	require.True(t, ok)
	require.Equal(t, 3, calls)

	// A full cache must remain bounded after inserting a new result.
	for i := range userJIDCacheSize {
		cache.entries[userJIDCacheKey{phone: time.Unix(int64(i), 0).String()}] = userJIDCacheEntry{}
	}
	_, ok = cache.resolve(&whatsmeow.Client{}, "5500912345678", clock, lookup)
	require.True(t, ok)
	require.Len(t, cache.entries, 1)
}

func TestUserJIDCacheDoesNotCacheFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		results []types.IsOnWhatsAppResponse
		err     error
	}{
		{"lookup error", nil, errors.New("usync failed")},
		{"empty response", nil, nil},
		{"not registered", []types.IsOnWhatsAppResponse{{IsIn: false}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache userJIDCache
			calls := 0
			for range 2 {
				_, ok := cache.resolve(nil, "5500912345678", time.Now, func(context.Context, []string) ([]types.IsOnWhatsAppResponse, error) {
					calls++
					return tc.results, tc.err
				})
				require.False(t, ok)
			}
			require.Equal(t, 2, calls)
		})
	}
}

func TestValidateJidWithLoginAccountLookup(t *testing.T) {
	original := config.WhatsappAccountValidation
	t.Cleanup(func() { config.WhatsappAccountValidation = original })
	asked := types.NewJID("5500912345678", types.DefaultUserServer)
	account := types.NewJID("550012345678", types.DefaultUserServer)
	group := types.NewJID("42", types.GroupServer)
	for _, tc := range []struct {
		name, input string
		enabled     bool
		sanitize    bool
		wantLookup  bool
		want        types.JID
	}{
		{"explicit phone JID", asked.String(), true, false, true, account},
		{"bare digits as in REST SendText", asked.User, true, true, true, account},
		{"bare digits passed directly", asked.User, true, false, false, asked},
		{"group", group.String(), true, false, false, group},
		{"validation disabled", asked.String(), false, false, false, asked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config.WhatsappAccountValidation = tc.enabled
			input := tc.input
			if tc.sanitize {
				SanitizePhone(&input)
			}
			var cache userJIDCache
			calls := 0
			got, err := validateJID(nil, input, func(client *whatsmeow.Client, phone string) (types.JID, bool) {
				return cache.resolve(client, phone, time.Now, func(_ context.Context, phones []string) ([]types.IsOnWhatsAppResponse, error) {
					calls++
					require.Equal(t, []string{"+" + asked.User}, phones)
					return []types.IsOnWhatsAppResponse{{IsIn: true, PhoneNumber: account}}, nil
				})
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			if tc.wantLookup {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
	config.WhatsappAccountValidation = true
	got, err := validateJID(nil, asked.String(), func(*whatsmeow.Client, string) (types.JID, bool) {
		return types.JID{}, false
	})
	require.ErrorContains(t, err, "Phone "+asked.String()+" is not on whatsapp")
	require.True(t, got.IsEmpty())
}
