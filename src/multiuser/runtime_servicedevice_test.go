package multiuser

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// This file covers the real serviceID/servicePhone closures runtime.go's
// Setup builds (around the `serviceID := func(ctx context.Context) string {`
// line) - not mocks of signin.Handler.ServiceDeviceID or web.Deps.ServicePhone,
// which every other test in this codebase hardcodes. Setup hands the exact
// same closures back on the *Runtime it returns (rt.Signin.ServiceDeviceID,
// rt.Web.ServicePhone), so calling those IS calling the real functions,
// with no reimplementation anywhere in this file.
//
// Before 3b36d701, serviceID discarded store.Setting's error entirely
// (`v, _, _ := st.Setting(...)`), so a transient read failure looked
// identical to "nothing configured" - a real, correctly-phrased sign-in
// message from a real contact was silently dropped with zero trace in the
// logs. The fix made it log; these tests are the coverage that fix never had.

func newServiceDeviceTestRuntime(t *testing.T, dm *whatsapp.DeviceManager) *Runtime {
	t.Helper()
	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := Setup(s, dm, nil, t.TempDir())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

// Happy path, 1 of 2: serviceID resolves a configured device id and logs
// nothing - there is nothing wrong to report.
func TestServiceIDResolvesTheConfiguredDeviceWithNoErrorLogged(t *testing.T) {
	ctx := context.Background()
	container := newRuntimeTestSQLStore(t)
	dm := whatsapp.NewDeviceManager(container, nil, nil)
	rt := newServiceDeviceTestRuntime(t, dm)

	if err := rt.Store.SetSetting(ctx, store.SettingServiceDeviceID, "d_service", "test"); err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	hook := logrustest.NewLocal(logrus.StandardLogger())
	defer hook.Reset()

	if got := rt.Signin.ServiceDeviceID(ctx); got != "d_service" {
		t.Fatalf("ServiceDeviceID = %q, want %q", got, "d_service")
	}
	for _, entry := range hook.AllEntries() {
		if entry.Level <= logrus.WarnLevel {
			t.Fatalf("unexpected log entry on the happy path: %s", entry.Message)
		}
	}
}

// Happy path, 2 of 2: servicePhone resolves the configured, logged-in
// device's real phone number, with no leading "+".
//
// IsLoggedIn() only ever becomes true after a genuine WhatsApp authentication
// handshake (go.mau.fi/whatsmeow's handleConnectSuccess, reached only over a
// live socket), and whatsmeow exposes no test hook for it. forceLoggedIn
// below is the one place in this suite that reaches past an exported method
// into that third-party client's private flag - the documented-safe
// reflect+unsafe idiom (unsafe package doc, case 6: converting the result of
// reflect.Value.UnsafeAddr in a single expression), not a hack invented for
// this test. Without it, this path could only ever be exercised by a mock of
// the interface, which is exactly what this file exists to stop doing.
func TestServicePhoneReturnsTheRealLoggedInNumberWithoutAPlusPrefix(t *testing.T) {
	ctx := context.Background()
	container := newRuntimeTestSQLStore(t)
	jid := types.NewADJID("27825550188", types.WhatsAppDomain, 1)
	dev := newRuntimeTestStoreDevice(t, container, jid, "Service")
	dm := whatsapp.NewDeviceManager(container, nil, nil)
	client := whatsmeow.NewClient(dev, nil)
	inst := whatsapp.NewDeviceInstance("d_service", client, nil)
	forceLoggedIn(t, client)
	inst.UpdateStateFromClient() // real path to populate PhoneNumber() from the paired store device
	dm.AddDevice(inst)

	if !inst.IsLoggedIn() {
		t.Fatal("forceLoggedIn did not take effect - precondition for this test is broken")
	}

	rt := newServiceDeviceTestRuntime(t, dm)
	if err := rt.Store.SetSetting(ctx, store.SettingServiceDeviceID, "d_service", "test"); err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	got := rt.Web.ServicePhone(ctx)
	if got != "27825550188" {
		t.Fatalf("ServicePhone = %q, want %q", got, "27825550188")
	}
	if strings.HasPrefix(got, "+") {
		t.Fatalf("ServicePhone kept a + prefix: %q", got)
	}
}

// Not configured: the legitimate "nothing set up yet" case. Both closures
// answer "" and - unlike a genuine read failure - nothing is logged.
func TestNeitherClosureErrorsOrLogsWhenNothingIsConfigured(t *testing.T) {
	ctx := context.Background()
	container := newRuntimeTestSQLStore(t)
	dm := whatsapp.NewDeviceManager(container, nil, nil)
	rt := newServiceDeviceTestRuntime(t, dm)

	hook := logrustest.NewLocal(logrus.StandardLogger())
	defer hook.Reset()

	if got := rt.Signin.ServiceDeviceID(ctx); got != "" {
		t.Fatalf("ServiceDeviceID = %q, want empty", got)
	}
	if got := rt.Web.ServicePhone(ctx); got != "" {
		t.Fatalf("ServicePhone = %q, want empty", got)
	}
	for _, entry := range hook.AllEntries() {
		if entry.Level <= logrus.WarnLevel {
			t.Fatalf("not-configured must stay silent, got: %s", entry.Message)
		}
	}
}

// The regression this suite exists for: a genuine store.Setting error.
// Today's fix (3b36d701) made serviceID log a real read failure instead of
// swallowing it; this closes the store's DB underneath a real seeded value
// so the SELECT genuinely fails at the driver level - never a fabricated
// error - and confirms that failure's actual shape before trusting an
// assertion built on it, per house convention.
func TestServiceIDLogsAndStaysEmptyOnAGenuineStoreError(t *testing.T) {
	ctx := context.Background()
	container := newRuntimeTestSQLStore(t)
	dm := whatsapp.NewDeviceManager(container, nil, nil)
	rt := newServiceDeviceTestRuntime(t, dm)

	if err := rt.Store.SetSetting(ctx, store.SettingServiceDeviceID, "d_service", "test"); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	if got := rt.Signin.ServiceDeviceID(ctx); got != "d_service" {
		t.Fatalf("sanity check before closing the db: ServiceDeviceID = %q, want %q", got, "d_service")
	}

	if err := rt.Store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Confirm the failure's shape before relying on it: a genuine,
	// driver-level error, never sql.ErrNoRows (Setting already turns that
	// into ("", false, nil), which must never be confused with a real
	// failure - that confusion is exactly what shipped before 3b36d701).
	if _, _, err := rt.Store.Setting(ctx, store.SettingServiceDeviceID); err == nil {
		t.Fatal("expected Setting to fail against a closed database - precondition is broken")
	} else if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected a genuine driver error, got sql.ErrNoRows: %v", err)
	}

	hook := logrustest.NewLocal(logrus.StandardLogger())
	defer hook.Reset()

	if got := rt.Signin.ServiceDeviceID(ctx); got != "" {
		t.Fatalf("ServiceDeviceID on a store error = %q, want empty (unchanged behaviour - the fix is visibility, not a new fallback)", got)
	}

	logged := false
	for _, entry := range hook.AllEntries() {
		if entry.Level <= logrus.ErrorLevel && strings.Contains(entry.Message, "service number setting") {
			logged = true
		}
	}
	if !logged {
		t.Fatal("expected the store error to be logged; the whole point of today's fix is that this is no longer silent")
	}
}

// forceLoggedIn flips the real whatsmeow client's private isLoggedIn flag
// without a network handshake. See the comment on
// TestServicePhoneReturnsTheRealLoggedInNumberWithoutAPlusPrefix for why.
func forceLoggedIn(t *testing.T, cli *whatsmeow.Client) {
	t.Helper()
	field := reflect.ValueOf(cli).Elem().FieldByName("isLoggedIn")
	if !field.IsValid() {
		t.Fatal("go.mau.fi/whatsmeow.Client no longer has an isLoggedIn field; update forceLoggedIn")
	}
	(*atomic.Bool)(unsafe.Pointer(field.UnsafeAddr())).Store(true)
}
