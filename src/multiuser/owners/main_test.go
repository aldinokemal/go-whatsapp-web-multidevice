package owners

import (
	"os"
	"testing"
	"time"
)

// TestMain runs this package's tests with the process zone at UTC+2, whatever
// TZ the machine has: accounts.db compares timestamps as UTC text, so a local
// time.Now() that reaches it fails here and not only off a UTC machine.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("SAST", 2*60*60)
	os.Exit(m.Run())
}
