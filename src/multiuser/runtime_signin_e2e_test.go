package multiuser

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// TestASignInMessageOnTheRealServiceDeviceGetsALinkEndToEnd is the one test
// that would have given a reader of this suite confidence the 3b36d701
// production incident could not happen: a correctly-configured service
// device, sent the exact sign-in phrase, actually gets a reply containing a
// sign-in link - routed there by the REAL serviceID closure Setup built
// (rt.Signin.ServiceDeviceID), not a hardcoded fake id the way every other
// test in multiuser/signin/handler_test.go wires it.
//
// It goes through Handler.OnEvent, not Handle: OnEvent is the only entry
// point that actually consults ServiceDeviceID to decide whether to answer
// on this device at all (Handle, which every existing signin test calls
// directly, never looks at it). The only thing swapped out is Send, to a
// recorder in place of a real WhatsApp socket - exactly the same seam every
// other signin test already uses.
func TestASignInMessageOnTheRealServiceDeviceGetsALinkEndToEnd(t *testing.T) {
	ctx := context.Background()
	container := newRuntimeTestSQLStore(t)

	svcJID := types.NewADJID("27800000000", types.WhatsAppDomain, 1)
	svcDev := newRuntimeTestStoreDevice(t, container, svcJID, "WTF service")
	dm := whatsapp.NewDeviceManager(container, nil, nil)
	svcInst := whatsapp.NewDeviceInstance("d_service_real", whatsmeow.NewClient(svcDev, nil), nil)
	dm.AddDevice(svcInst)

	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := Setup(s, dm, nil, t.TempDir())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	// Configure the real service device id through the real store - the
	// exact row serviceID (rt.Signin.ServiceDeviceID) reads.
	if err := rt.Store.SetSetting(ctx, store.SettingServiceDeviceID, "d_service_real", "test"); err != nil {
		t.Fatalf("set service device id: %v", err)
	}

	// Pre-create the user so this test is deterministic regardless of
	// SignupOpen; nothing about ServiceDeviceID routing depends on this.
	from := "+27825550101"
	if _, err := rt.Store.CreateUser(ctx, from); err != nil {
		t.Fatalf("create user: %v", err)
	}

	rec := &e2eSentRecorder{}
	rt.Signin.Send = rec // the only field on rt.Signin this test overrides

	fromJID := types.NewJID("27825550101", types.DefaultUserServer)
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: fromJID, Sender: fromJID},
			ID:            "M1",
		},
		Message: &waE2E.Message{Conversation: proto.String("Sign me in to WTF")},
	}

	// The real, routed entry point: OnEvent calls rt.Signin.ServiceDeviceID
	// (the real serviceID closure) to decide whether svcInst is the service
	// device before doing anything else.
	rt.Signin.OnEvent(ctx, svcInst, evt)

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) && rec.count() == 0 {
		time.Sleep(50 * time.Millisecond)
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("expected exactly one reply, got %d", got)
	}
	reply := rec.last()
	if reply.to != fromJID {
		t.Fatalf("reply went to %s, want %s", reply.to, fromJID)
	}
	if !strings.Contains(reply.text, "https://wtf.test/signin/verify?t=") {
		t.Fatalf("expected a sign-in link in the reply, got %q", reply.text)
	}

	// Negative control: the identical message, arriving on a device the real
	// closure does NOT resolve to, never gets a reply - proving
	// ServiceDeviceID's id is actually what gates this, not merely a filter
	// that happens to pass.
	otherInst := whatsapp.NewDeviceInstance("d_not_the_service_device", nil, nil)
	rt.Signin.OnEvent(ctx, otherInst, evt)
	time.Sleep(150 * time.Millisecond)
	if got := rec.count(); got != 1 {
		t.Fatalf("expected no additional reply from a non-service device, got %d", got)
	}
}

type e2eSent struct {
	to   types.JID
	text string
}

// e2eSentRecorder is the same seam multiuser/signin/handler_test.go's
// recorder fills (signin.Sender): everything else about rt.Signin stays the
// real Handler Setup built, ServiceDeviceID included.
type e2eSentRecorder struct {
	mu   sync.Mutex
	msgs []e2eSent
}

func (r *e2eSentRecorder) SendText(_ context.Context, _ *whatsapp.DeviceInstance, to types.JID, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, e2eSent{to, text})
	return nil
}

func (r *e2eSentRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

func (r *e2eSentRecorder) last() e2eSent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.msgs[len(r.msgs)-1]
}
