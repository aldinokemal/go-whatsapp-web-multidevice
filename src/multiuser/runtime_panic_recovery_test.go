package multiuser

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"go.mau.fi/whatsmeow/types/events"
)

// TestSetupGoroutinePanicIsRecoveredNotFatal is the proof that a panic deep in
// the duplicate-phone guard's call chain (owners.GuardDuplicatePhone,
// PairSource.Paired, phoneNumber, or anywhere else reached from the goroutine
// Setup spawns on *events.PairSuccess) does not take the whole process down
// with it.
//
// Go does not let a deferred recover() in one goroutine catch a panic raised
// in a different goroutine, so the only place that can ever catch this
// goroutine's own panic is a recover() inside the goroutine itself. Without
// one, this test crashes the entire `go test` binary (not just fails this
// test) the moment the injected panic unwinds - that crash is what proves the
// gap, and the fix is what stops it from happening.
//
// guardDuplicatePhoneForTest is the seam: it stands in for
// owners.GuardDuplicatePhone (both of the real call's actual collaborators,
// GuardDuplicatePhone and the production PairSource/store it is handed, are
// deliberately defensive and never panic on bad input - every realistic
// panic down this path would come from something unexpected deeper in a real
// store/whatsmeow failure). The seam reproduces that failure mode exactly
// where production code would hit it, without needing to engineer a genuine
// fault.
func TestSetupGoroutinePanicIsRecoveredNotFatal(t *testing.T) {
	ctx := context.Background()
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	inst := whatsapp.NewDeviceInstance("d_panic", nil, nil)
	dm.AddDevice(inst)

	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := Setup(s, dm, nil, t.TempDir())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if whatsapp.EventHook == nil {
		t.Fatal("Setup must install whatsapp.EventHook")
	}

	entered := make(chan struct{})
	guardDuplicatePhoneForTest = func(context.Context, owners.PairSource, owners.OwnerLookup, string) (bool, error) {
		close(entered)
		panic("simulated panic deep in the duplicate guard's call chain")
	}
	t.Cleanup(func() { guardDuplicatePhoneForTest = nil })

	whatsapp.EventHook(ctx, inst, &events.PairSuccess{})

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the spawned goroutine never reached the guard")
	}

	// Give the panicking goroutine time to unwind. An unrecovered panic here
	// crashes the whole test binary before this line is ever reached - there
	// is no assertion to write for that case, the crash itself is the
	// failure. Reaching the end of the test is the proof the fix held.
	time.Sleep(200 * time.Millisecond)
}
