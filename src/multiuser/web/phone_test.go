package web

import "testing"

func TestDisplayPhoneGroupsDigits(t *testing.T) {
	for in, want := range map[string]string{
		"+27825550100":   "+27 82 555 0100",
		"15555550142":    "+1 555 555 0142",
		"+447700900123":  "+44 770 090 0123",
		"+4915112345678": "+49 1511 234 5678",
		"+97150123456":   "+971 5012 3456",
		"+3512345678":    "+351 234 5678",
		"":               "",
		"abc":            "abc",
	} {
		if got := displayPhone(in); got != want {
			t.Errorf("displayPhone(%q) = %q, want %q", in, got, want)
		}
	}
}
