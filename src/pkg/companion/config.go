// Package companion implements bounded, opt-in Android companion recovery.
// It does not initialize the GoWA server or access its credential databases.
package companion

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"time"
)

type Config struct {
	SchemaVersion int       `json:"schema_version"`
	Accounts      []Account `json:"accounts"`
}
type BackendConfig struct {
	ID      string   `json:"id"`
	Command []string `json:"command"`
}
type PhoneConfig struct {
	Command []string       `json:"command"`
	Config  map[string]any `json:"config"`
}
type Account struct {
	ID            string          `json:"id"`
	Phone         string          `json:"phone"`
	DisplayName   string          `json:"display_name"`
	NotBefore     string          `json:"not_before,omitempty"`
	VerifySeconds int             `json:"verify_seconds,omitempty"`
	Backends      []BackendConfig `json:"backends"`
	PhoneAdapter  PhoneConfig     `json:"phone_adapter"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var phoneNumber = regexp.MustCompile(`^[1-9][0-9]{7,14}$`)
var pairingCode = regexp.MustCompile(`^[A-Z0-9]{4}-?[A-Z0-9]{4}$`)

func validCommand(argv []string) bool {
	if len(argv) == 0 || argv[0] == "" {
		return false
	}
	for _, s := range argv {
		for _, c := range s {
			if c == 0 {
				return false
			}
		}
	}
	return true
}
func (a Account) notBefore() (float64, error) {
	if a.NotBefore == "" {
		return 0, nil
	}
	t, e := time.Parse(time.RFC3339, a.NotBefore)
	return float64(t.Unix()), e
}
func (c Config) Validate() error {
	invalid := errors.New("invalid recovery configuration")
	if c.SchemaVersion != 1 || len(c.Accounts) == 0 {
		return invalid
	}
	names := map[string]bool{}
	for _, a := range c.Accounts {
		if !identifier.MatchString(a.ID) || names[a.ID] || !phoneNumber.MatchString(a.Phone) || a.DisplayName == "" || len(a.Backends) < 1 || len(a.Backends) > 4 || !validCommand(a.PhoneAdapter.Command) || a.VerifySeconds < 0 || a.VerifySeconds > 600 {
			return invalid
		}
		names[a.ID] = true
		if _, err := a.notBefore(); err != nil {
			return invalid
		}
		ids := map[string]bool{}
		for _, b := range a.Backends {
			if !identifier.MatchString(b.ID) || b.ID == "phone" || ids[b.ID] || !validCommand(b.Command) {
				return invalid
			}
			ids[b.ID] = true
		}
	}
	return nil
}
func LoadConfig(path string) (Config, error) {
	var c Config
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&c); e != nil {
		return c, e
	}
	if e = decoder.Decode(new(any)); e != io.EOF {
		return c, errors.New("trailing recovery configuration")
	}
	return c, c.Validate()
}
