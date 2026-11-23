package multiuser

import (
	"errors"
	"strconv"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
)

// defaultMaxDevices matches the users.max_devices column default.
const defaultMaxDevices = 2

// loadCapacity reads WTF_MAX_PHONES and WTF_DEFAULT_MAX_DEVICES. Open sign-up
// without a server-wide cap is refused: strangers could link numbers until
// the instance runs out of memory.
func loadCapacity(getenv func(string) string, s *Settings) error {
	if v := strings.TrimSpace(getenv("WTF_MAX_PHONES")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return errors.New("WTF_MAX_PHONES must be a whole number, 0 or more (0 means no cap)")
		}
		s.MaxPhones = n
	}
	if v := strings.TrimSpace(getenv("WTF_DEFAULT_MAX_DEVICES")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > store.MaxDevicesLimit {
			return errors.New("WTF_DEFAULT_MAX_DEVICES must be a whole number from 1 to " + strconv.Itoa(store.MaxDevicesLimit))
		}
		s.DefaultMaxDevices = n
	}
	if s.SignupOpen && s.MaxPhones == 0 {
		return errors.New("WTF_SIGNUP_OPEN=true needs WTF_MAX_PHONES set above 0")
	}
	return nil
}
