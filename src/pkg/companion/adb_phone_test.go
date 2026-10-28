package companion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type adbWorld struct {
	on, locked  bool
	user        string
	packageName string
	commands    []string
}

func (w *adbWorld) execute(_ context.Context, args []string, data []byte) ([]byte, error) {
	cmd := string(data)
	w.commands = append(w.commands, strings.Join(args, " ")+" "+cmd)
	if args[0] == "get-state" {
		return []byte("device\n"), nil
	}
	if args[0] == "exec-out" {
		return []byte("\x89PNG\r\n\x1a\nsynthetic"), nil
	}
	switch {
	case strings.Contains(cmd, "'dumpsys' 'window' 'policy'"):
		locked := "false"
		if w.locked {
			locked = "true"
		}
		return []byte("KeyguardServiceDelegate\n showing=" + locked), nil
	case strings.Contains(cmd, "'dumpsys' 'power'"):
		power := "OFF"
		if w.on {
			power = "ON"
		}
		return []byte("Display Power: state=" + power), nil
	case strings.Contains(cmd, "'dumpsys' 'activity' 'activities'"):
		return []byte("topResumedActivity= u" + w.user + " " + w.packageName + "/Main}"), nil
	case strings.Contains(cmd, "'input' 'keyevent' '224'"):
		w.on = true
	case strings.Contains(cmd, "'input' 'keyevent' '223'"):
		w.on = false
	case strings.Contains(cmd, "'resolve-activity'"):
		return []byte(testPackage + "/Main"), nil
	case strings.Contains(cmd, "'cat'"):
		return []byte(screenXML(node("Chats", `bounds="[0,0][720,1600]"`))), nil
	}
	return nil, nil
}
func adbSetup(t *testing.T, w *adbWorld) *ADB {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	transport, e := NewADB(Object{"serial": "EXAMPLE", "user": 0, "package": testPackage, "state_dir": dir}, Account{})
	if e != nil {
		t.Fatal(e)
	}
	a := transport.(*ADB)
	a.execute = w.execute
	t.Cleanup(func() { a.Release() })
	return a
}
func TestADBRestoresDisplayAndReleasesSession(t *testing.T) {
	ctx := context.Background()
	w := &adbWorld{user: "0", packageName: testPackage}
	a := adbSetup(t, w)
	if e := a.Start(ctx); e != nil {
		t.Fatal(e)
	}
	if !w.on {
		t.Fatal("did not wake")
	}
	if _, e := a.Capture(ctx); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(a.evidence)
	if e != nil {
		t.Fatal(e)
	}
	var png, xml bool
	for _, entry := range entries {
		info, e := entry.Info()
		if e != nil || info.Mode().Perm() != 0600 {
			t.Fatal(e, info)
		}
		png = png || strings.HasSuffix(entry.Name(), ".png")
		xml = xml || strings.HasSuffix(entry.Name(), ".xml")
	}
	if !png || !xml {
		t.Fatal(entries)
	}
	if e = a.Restore(ctx, "Chats"); e != nil || w.on {
		t.Fatal(e, w.on)
	}
	if e = a.Release(); e != nil {
		t.Fatal(e)
	}
	l, e := acquireLock(filepath.Join(a.directory, "EXAMPLE.lock"))
	if e != nil {
		t.Fatal(e)
	}
	l.Close()
}
func TestADBWrongProfileAppAndLockFailBeforeInput(t *testing.T) {
	for _, tc := range []struct {
		user, pkg, reason string
		locked            bool
	}{{"10", testPackage, "profile_mismatch", false}, {"0", "other.app", "phone_busy", false}, {"0", testPackage, "owner_verification", true}} {
		t.Run(tc.reason, func(t *testing.T) {
			w := &adbWorld{user: tc.user, packageName: tc.pkg, locked: tc.locked, on: true}
			a := adbSetup(t, w)
			e := a.Start(context.Background())
			b, ok := e.(*Blocked)
			if !ok || b.Reason != tc.reason {
				t.Fatal(e)
			}
			for _, cmd := range w.commands {
				if strings.Contains(cmd, "'input'") || strings.Contains(cmd, "'am'") {
					t.Fatal(cmd)
				}
			}
		})
	}
}
func TestADBSleepingLockedPhoneRestoresSleep(t *testing.T) {
	w := &adbWorld{user: "0", packageName: testPackage, locked: true}
	a := adbSetup(t, w)
	if e := a.Start(context.Background()); e == nil {
		t.Fatal("bypassed lock")
	}
	if e := a.Restore(context.Background(), ""); e != nil || w.on {
		t.Fatal(e, w.on)
	}
}
func TestADBGuardStopsAfterProfileSwitch(t *testing.T) {
	w := &adbWorld{user: "0", packageName: testPackage}
	a := adbSetup(t, w)
	if e := a.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	w.user = "10"
	if e := a.Text(context.Background(), "A"); e == nil {
		t.Fatal("typed in other profile")
	}
	for _, cmd := range w.commands {
		if strings.Contains(cmd, "'text'") {
			t.Fatal(cmd)
		}
	}
}

func TestADBCaptureWakesAfterDisplayTimeoutAndRestoresOriginalSleep(t *testing.T) {
	w := &adbWorld{user: "0", packageName: testPackage}
	a := adbSetup(t, w)
	ctx := context.Background()
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	w.on = false
	w.commands = nil
	if _, err := a.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	if !w.on || !strings.Contains(w.commands[0], "'keyevent' '224'") {
		t.Fatal(w.commands)
	}
	w.on = false
	if err := a.Restore(ctx, "Chats"); err != nil || w.on {
		t.Fatal(err, w.on)
	}
}
func TestADBCaptureWakeNeverBypassesProtectedLockOrProfile(t *testing.T) {
	for _, mode := range []string{"lock", "profile", "app"} {
		t.Run(mode, func(t *testing.T) {
			w := &adbWorld{user: "0", packageName: testPackage}
			a := adbSetup(t, w)
			ctx := context.Background()
			if err := a.Start(ctx); err != nil {
				t.Fatal(err)
			}
			w.on = false
			switch mode {
			case "lock":
				w.locked = true
			case "profile":
				w.user = "10"
			case "app":
				w.packageName = "other.app"
			}
			w.commands = nil
			if _, err := a.Capture(ctx); err == nil {
				t.Fatal("captured protected screen")
			}
			for _, cmd := range w.commands {
				if strings.Contains(cmd, "exec-out") || strings.Contains(cmd, "'tap'") || strings.Contains(cmd, "'text'") || strings.Contains(cmd, "'uiautomator'") {
					t.Fatal(cmd)
				}
			}
		})
	}
}
