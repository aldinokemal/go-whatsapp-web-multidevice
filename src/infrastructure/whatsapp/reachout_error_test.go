package whatsapp

import (
	"errors"
	"testing"

	"go.mau.fi/whatsmeow"
)

// reachoutErr mimics the error whatsmeow produces for server error 463:
// fmt.Errorf("%w %d", ErrServerReturnedError, 463).
func reachoutErr() error {
	return errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error 463"))
}

func nonReachoutServerErr(code string) error {
	return errors.Join(whatsmeow.ErrServerReturnedError, errors.New("server returned error "+code))
}

func TestIsReachoutTimelockError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"wrapped 463", reachoutErr(), true},
		{"wrapped non-463", nonReachoutServerErr("500"), false},
		{"unrelated error", errors.New("network unreachable"), false},
		{"bare 463 string without sentinel", errors.New("error 463"), false},
		{"sentinel but no 463", nonReachoutServerErr("400"), false},
		{"superstring 4631 must not match", nonReachoutServerErr("4631"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReachoutTimelockError(tt.err); got != tt.want {
				t.Fatalf("IsReachoutTimelockError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
