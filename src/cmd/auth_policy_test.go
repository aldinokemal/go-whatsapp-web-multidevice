package cmd

import "testing"

func TestBasicAuthComplete(t *testing.T) {
	for _, tt := range []struct {
		credentials []string
		want        bool
	}{
		{nil, false},
		{[]string{"admin:secret"}, true},
		{[]string{"admin:secret", "guest:pass"}, true},
		{[]string{"admin:"}, false},
		{[]string{":secret"}, false},
		{[]string{"admin"}, false},
		{[]string{"admin:secret", "guest:"}, false},
	} {
		if got := basicAuthComplete(tt.credentials); got != tt.want {
			t.Errorf("basicAuthComplete(%q) = %v, want %v", tt.credentials, got, tt.want)
		}
	}
}

func TestUnauthenticatedBindAllowed(t *testing.T) {
	cases := []struct {
		name           string
		host           string
		hasCredentials bool
		allowInsecure  bool
		wantAllowed    bool
	}{
		{name: "credentials on a public bind", host: "0.0.0.0", hasCredentials: true, wantAllowed: true},
		{name: "loopback without credentials", host: "127.0.0.1", wantAllowed: true},
		{name: "localhost without credentials", host: "localhost", wantAllowed: true},
		{name: "ipv6 loopback without credentials", host: "::1", wantAllowed: true},
		{name: "bracketed ipv6 loopback", host: "[::1]", wantAllowed: true},
		{name: "all interfaces without credentials", host: "0.0.0.0", wantAllowed: false},
		{name: "public address without credentials", host: "192.0.2.10", wantAllowed: false},
		{name: "unspecified ipv6 without credentials", host: "::", wantAllowed: false},
		{name: "empty host without credentials", host: "", wantAllowed: false},
		{name: "public bind with explicit insecure switch", host: "0.0.0.0", allowInsecure: true, wantAllowed: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := unauthenticatedBindAllowed(tt.host, tt.hasCredentials, tt.allowInsecure)
			if got != tt.wantAllowed {
				t.Fatalf("unauthenticatedBindAllowed(%q, creds=%v, allow=%v) = %v, want %v", tt.host, tt.hasCredentials, tt.allowInsecure, got, tt.wantAllowed)
			}
		})
	}
}
