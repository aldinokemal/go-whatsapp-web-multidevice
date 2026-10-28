package companion

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testPackage = "com.whatsapp.w4b"

func node(text, extra string) string {
	return `<node package="com.whatsapp.w4b" class="android.widget.TextView" text="` + text + `" ` + extra + ` />`
}
func screenXML(nodes ...string) string {
	return "<hierarchy>" + strings.Join(nodes, "") + "</hierarchy>"
}

const actionable = `clickable="true" enabled="true" bounds="[10,10][110,60]"`

func TestScreenRestrictionAndOwnerGuards(t *testing.T) {
	for _, tc := range []struct {
		text, reason string
		delay        int
	}{{"Your account is restricted right now", "restriction", 21600}, {"Your account is restricted right now 04:33:59", "restriction", 16739}, {"Temporarily banned", "restriction", 21600}, {"Enter your PIN", "owner_verification", 0}, {"Verify your phone number", "owner_verification", 0}, {"Verify your identity", "owner_verification", 0}, {"Fingerprint", "owner_verification", 0}, {"Registration code", "owner_verification", 0}, {"Enter the 6-digit code", "owner_verification", 0}, {"Two-step verification", "owner_verification", 0}} {
		t.Run(tc.text, func(t *testing.T) {
			_, e := ParseScreen(screenXML(node(tc.text, "")), testPackage)
			b, ok := e.(*Blocked)
			if !ok || b.Reason != tc.reason || b.RetryAfter != tc.delay {
				t.Fatal(e)
			}
		})
	}
}
func TestScreenRejectsWrongAppMalformedAndOversizedXML(t *testing.T) {
	for _, source := range []string{`<hierarchy><node package="com.android.settings" /></hierarchy>`, `<hierarchy><node>`, strings.Repeat("x", 2_000_001), `<hierarchy />`} {
		if _, e := ParseScreen(source, testPackage); e == nil {
			t.Fatal("accepted unknown screen")
		}
	}
}
func TestPopupEnabledNonClickableRowRegression(t *testing.T) {
	s, e := ParseScreen(`<hierarchy><node package="com.whatsapp.w4b" class="android.widget.ListView" clickable="false" enabled="true"><node content-desc="Settings" class="android.widget.LinearLayout" clickable="false" enabled="true" bounds="[320,948][712,1044]" /></node></hierarchy>`, testPackage)
	if e != nil {
		t.Fatal(e)
	}
	x, y, e := s.Unique("Settings")
	if e != nil || x != 516 || y != 996 {
		t.Fatal(x, y, e)
	}
}
func TestSelectedChatsTabRegression(t *testing.T) {
	s, e := ParseScreen(screenXML(node("WhatsApp", ""), `<node package="com.whatsapp.w4b" content-desc="Chats, 58 new notifications" selected="true" class="android.widget.FrameLayout" />`), testPackage)
	if e != nil || !s.Title("Chats") || s.Title("Updates") {
		t.Fatal(s, e)
	}
}
func TestUnknownAmbiguousAndDisabledTargets(t *testing.T) {
	for _, source := range []string{screenXML(node("LOG IN", "")), screenXML(node("LOG IN", actionable), node("LOG IN", `clickable="true" enabled="true" bounds="[120,10][180,60]"`)), screenXML(node("LOG IN", `clickable="true" enabled="false" bounds="[10,10][110,60]"`))} {
		s, e := ParseScreen(source, testPackage)
		if e != nil {
			t.Fatal(e)
		}
		if _, _, e = s.Unique("LOG IN"); e == nil {
			t.Fatal("accepted ambiguous target")
		}
	}
}
func TestCodeNeedsVisibleField(t *testing.T) {
	s, e := ParseScreen(screenXML(node("Enter code", "")), testPackage)
	if e != nil || s.IsCode() {
		t.Fatal(s, e)
	}
}

type uiTransport struct {
	screens  []string
	index    int
	actions  []string
	restored string
	released bool
}

func (t *uiTransport) Start(context.Context) error {
	t.actions = append(t.actions, "start")
	return nil
}
func (t *uiTransport) Capture(context.Context) (string, error) {
	if t.index >= len(t.screens) {
		return "", block("unknown_screen")
	}
	s := t.screens[t.index]
	t.index++
	return s, nil
}
func (t *uiTransport) Tap(_ context.Context, x, y int) error {
	t.actions = append(t.actions, fmt.Sprintf("tap:%d:%d", x, y))
	return nil
}
func (t *uiTransport) Back(context.Context) error { t.actions = append(t.actions, "back"); return nil }
func (t *uiTransport) Swipe(context.Context) error {
	t.actions = append(t.actions, "swipe")
	return nil
}
func (t *uiTransport) Text(_ context.Context, s string) error {
	t.actions = append(t.actions, "text:"+s)
	return nil
}
func (t *uiTransport) Restore(_ context.Context, s string) error { t.restored = s; return nil }
func (t *uiTransport) Release() error                            { t.released = true; return nil }
func codeScreen(focused bool) string {
	return screenXML(node("Enter code", ""), fmt.Sprintf(`<node package="com.whatsapp.w4b" class="android.widget.EditText" focused="%t" />`, focused))
}
func settingsScreen() string {
	return screenXML(node("Settings", ""), node("Linked devices", actionable), node("Example Sales", actionable))
}
func linkedScreen(extra ...string) string {
	return screenXML(append([]string{node("Linked devices", "")}, extra...)...)
}
func TestUIIdentityRequiresNameAndActualNumber(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			number := "+1 555 123 4567"
			if !valid {
				number = "+1 555 999 9999"
			}
			transport := &uiTransport{screens: []string{settingsScreen(), screenXML(node("Other profile", ""), node(number, "")), settingsScreen(), linkedScreen()}}
			u := &PairingUI{Transport: transport, Account: testAccount(), Package: testPackage}
			out, e := u.Begin(context.Background())
			if valid {
				if e != nil || out["identity_verified"] != true {
					t.Fatal(out, e)
				}
			} else {
				if e == nil || u.identityVerified {
					t.Fatal("accepted different number")
				}
			}
			if e = u.Finish(context.Background()); e != nil || !transport.released || transport.restored != "Settings" {
				t.Fatal(e, transport)
			}
		})
	}
}
func TestUIRejectsUnverifiedOrBusyScreen(t *testing.T) {
	u := &PairingUI{}
	if _, e := u.Prepare(context.Background()); e == nil {
		t.Fatal("prepared without identity")
	}
	transport := &uiTransport{screens: []string{codeScreen(true)}}
	u = &PairingUI{Transport: transport, Account: testAccount(), Package: testPackage}
	if _, e := u.Begin(context.Background()); e == nil {
		t.Fatal("accepted busy screen")
	}
}
func TestUINameMismatchStopsBeforeProfileTap(t *testing.T) {
	transport := &uiTransport{screens: []string{screenXML(node("Settings", ""), node("Linked devices", actionable), node("Different Sales", actionable))}}
	u := &PairingUI{Transport: transport, Account: testAccount(), Package: testPackage}
	_, e := u.Begin(context.Background())
	if e == nil || len(transport.actions) != 1 {
		t.Fatal(e, transport.actions)
	}
}
func TestUIPrepareCapacityAndAmbiguity(t *testing.T) {
	entry := `<node package="com.whatsapp.w4b" clickable="true" enabled="true" bounds="[10,10][100,100]">` + node("Google Chrome (GOWA v1.0.0)", "") + node("Logged out", "") + `</node>`
	for _, tc := range []struct{ source, reason string }{{linkedScreen(node("4 of 4 devices linked", ""), node("LINK A DEVICE", actionable)), "slots_full"}, {linkedScreen(entry, entry), "ambiguous_device"}, {linkedScreen(node("LINK A DEVICE", "")), "unknown_screen"}} {
		transport := &uiTransport{screens: []string{tc.source}}
		u := &PairingUI{Transport: transport, Account: testAccount(), Package: testPackage, identityVerified: true}
		_, e := u.Prepare(context.Background())
		b, ok := e.(*Blocked)
		if !ok || b.Reason != tc.reason || len(transport.actions) > 0 {
			t.Fatal(e, transport.actions)
		}
	}
}
func TestUIPrepareRestoresLoggedOutSlotAndTypesSingleCharacters(t *testing.T) {
	entry := `<node package="com.whatsapp.w4b" clickable="true" enabled="true" bounds="[10,10][100,100]">` + node("Google Chrome (GOWA v1.0.0)", "") + node("Logged out", "") + `</node>`
	transport := &uiTransport{screens: []string{linkedScreen(entry), screenXML(node("LOG IN", actionable)), screenXML(node("Link with phone number instead", actionable)), codeScreen(true), codeScreen(true), linkedScreen()}}
	u := &PairingUI{Transport: transport, Account: testAccount(), Package: testPackage, identityVerified: true, CharacterPause: time.Nanosecond}
	out, e := u.Prepare(context.Background())
	if e != nil || out["ready"] != true {
		t.Fatal(out, e)
	}
	out, e = u.Enter(context.Background(), "ABCD-1234")
	if e != nil || out["submitted"] != true {
		t.Fatal(out, e)
	}
	var typed []string
	for _, a := range transport.actions {
		if strings.HasPrefix(a, "text:") {
			typed = append(typed, strings.TrimPrefix(a, "text:"))
		}
	}
	if strings.Join(typed, "") != "ABCD1234" || len(typed) != 8 {
		t.Fatal(typed)
	}
	if _, e = u.Enter(context.Background(), "ABCD1234"); e == nil {
		t.Fatal("replayed code entry")
	}
}
func TestUIRequiresFocusedCodeField(t *testing.T) {
	transport := &uiTransport{screens: []string{codeScreen(false)}}
	u := &PairingUI{Transport: transport, Package: testPackage, prepared: true}
	_, e := u.Enter(context.Background(), "ABCD1234")
	if e == nil || len(transport.actions) > 0 {
		t.Fatal(e, transport.actions)
	}
}

func namingScreen(value string, focused bool) string {
	return screenXML(node("Device name", ""), node("This helps you assign and manage devices. Customers won't see it. You can always change it.", ""),
		fmt.Sprintf(`<node package="com.whatsapp.w4b" class="android.widget.EditText" text="%s" focused="%t" %s />`, value, focused, actionable), node("SAVE", actionable))
}
func TestUIPostLinkDeviceNameForm(t *testing.T) {
	transport := &uiTransport{screens: []string{codeScreen(true), namingScreen("", false), namingScreen("", true), namingScreen("GOWA", true), linkedScreen()}}
	u := &PairingUI{Transport: transport, Package: testPackage, prepared: true, CharacterPause: time.Nanosecond}
	out, err := u.Enter(context.Background(), "ABCD-1234")
	if err != nil || out["submitted"] != true {
		t.Fatal(out, err, transport.actions)
	}
	var typed string
	for _, action := range transport.actions {
		if strings.HasPrefix(action, "text:") {
			typed += strings.TrimPrefix(action, "text:")
		}
	}
	if typed != "ABCD1234GOWA" {
		t.Fatal(transport.actions)
	}
	if u.prepared {
		t.Fatal("code entry can be replayed")
	}
}
func TestUIPostLinkNamingFailsClosed(t *testing.T) {
	for name, source := range map[string]string{
		"existing name":         namingScreen("Existing", true),
		"other form":            strings.ReplaceAll(namingScreen("", true), "This helps you assign and manage devices.", "Verify your business details."),
		"multiple fields":       strings.Replace(namingScreen("", true), "</hierarchy>", `<node package="com.whatsapp.w4b" class="android.widget.EditText" /></hierarchy>`, 1),
		"multiple save buttons": strings.Replace(namingScreen("", true), "</hierarchy>", node("SAVE", actionable)+"</hierarchy>", 1),
		"owner verification":    strings.Replace(namingScreen("", true), "</hierarchy>", node("Enter your PIN", "")+"</hierarchy>", 1),
	} {
		t.Run(name, func(t *testing.T) {
			transport := &uiTransport{screens: []string{codeScreen(true), source}}
			u := &PairingUI{Transport: transport, Package: testPackage, prepared: true, CharacterPause: time.Nanosecond}
			_, err := u.Enter(context.Background(), "ABCD1234")
			if err == nil || len(transport.actions) != 8 {
				t.Fatal(err, transport.actions)
			}
		})
	}
}
func TestUIPostLinkNamingRequiresVerifiedInputBeforeSave(t *testing.T) {
	transport := &uiTransport{screens: []string{codeScreen(true), namingScreen("", true), namingScreen("GOW", true)}}
	u := &PairingUI{Transport: transport, Package: testPackage, prepared: true, CharacterPause: time.Nanosecond}
	_, err := u.Enter(context.Background(), "ABCD1234")
	if err == nil || len(transport.actions) != 12 {
		t.Fatal(err, transport.actions)
	}
	for _, action := range transport.actions {
		if strings.HasPrefix(action, "tap:") {
			t.Fatal("saved incomplete name")
		}
	}
}
func TestUIPostLinkNamingIsNotReplayed(t *testing.T) {
	transport := &uiTransport{screens: []string{codeScreen(true), namingScreen("", true), namingScreen("GOWA", true), namingScreen("", true), namingScreen("", true)}}
	u := &PairingUI{Transport: transport, Package: testPackage, prepared: true, CharacterPause: time.Nanosecond, PollInterval: time.Nanosecond}
	_, err := u.Enter(context.Background(), "ABCD1234")
	if err == nil || len(transport.actions) != 13 {
		t.Fatal(err, transport.actions)
	}
}
