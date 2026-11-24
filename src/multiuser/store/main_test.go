package store

import (
	"os"
	"testing"
	"time"
)

// TestMain runs this package's tests with the process zone at UTC+2, whatever
// TZ the machine has, so a time.Now() that reaches SQL without .UTC() fails
// here and not only on a reviewer's laptop. See utc.go.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("SAST", 2*60*60)
	os.Exit(m.Run())
}
