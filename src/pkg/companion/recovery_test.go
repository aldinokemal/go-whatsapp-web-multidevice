package companion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testPhone = "15551234567"

func testAccount() Account {
	return Account{ID: "sales", Phone: testPhone, DisplayName: "Example Sales", Backends: []BackendConfig{{ID: "primary", Command: []string{"primary"}}, {ID: "backup", Command: []string{"backup"}}}, PhoneAdapter: PhoneConfig{Command: []string{"phone"}}, VerifySeconds: 1}
}
func snapshot(connected bool, jid string) Object {
	return Object{"connected": connected, "logged_in": connected, "policy_ok": true, "saved_sessions": 0, "companion_jid": jid}
}

type testWorld struct {
	calls                           []string
	snapshots                       map[string]Object
	postPair                        map[string]Object
	fail                            map[string]string
	reconnect                       bool
	last                            string
	state                           *State
	t                               *testing.T
	beginVerified, ready, duplicate bool
}

func newWorld(t *testing.T, s *State) *testWorld {
	return &testWorld{t: t, state: s, snapshots: map[string]Object{"primary": snapshot(false, ""), "backup": snapshot(false, "")}, fail: map[string]string{}, beginVerified: true, ready: true}
}

type fakeAdapter struct {
	w    *testWorld
	name string
}

func (w *testWorld) factory(argv []string) (Adapter, error) {
	w.calls = append(w.calls, argv[0]+":open")
	return &fakeAdapter{w, argv[0]}, nil
}
func (a *fakeAdapter) Close() error { a.w.calls = append(a.w.calls, a.name+":close"); return nil }
func (a *fakeAdapter) Call(_ context.Context, op string, r Object) (Object, error) {
	w := a.w
	w.calls = append(w.calls, a.name+":"+op)
	if reason := w.fail[a.name+":"+op]; reason != "" {
		delay := 0
		if reason == "restriction" {
			delay = 3600
		}
		return nil, &Blocked{Reason: reason, RetryAfter: delay}
	}
	if a.name == "phone" {
		switch op {
		case "begin":
			return Object{"identity_verified": w.beginVerified}, nil
		case "prepare_pair":
			return Object{"ready": w.ready}, nil
		case "enter_code":
			jid := testPhone + ":1@s.whatsapp.net"
			if w.last == "backup" && !w.duplicate {
				jid = testPhone + ":2@s.whatsapp.net"
			}
			w.snapshots[w.last] = snapshot(true, jid)
			if override, ok := w.postPair[w.last]; ok {
				w.snapshots[w.last] = override
			}
		}
		return Object{}, nil
	}
	switch op {
	case "inspect":
		s := Object{}
		for k, v := range w.snapshots[a.name] {
			s[k] = v
		}
		return s, nil
	case "reconnect":
		if w.reconnect {
			w.snapshots[a.name] = snapshot(true, testPhone+":3@s.whatsapp.net")
		}
	case "pair_code":
		p, e := w.state.Get("sales", a.name)
		if e != nil || p.Phase != "pairing_started" {
			w.t.Fatal("code requested before durable intent")
		}
		w.last = a.name
		return Object{"pair_code": "ABCD-1234"}, nil
	}
	return Object{}, nil
}
func setupRecovery(t *testing.T) (*State, *testWorld, Runner) {
	t.Helper()
	directory := t.TempDir()
	if e := os.Chmod(directory, 0700); e != nil {
		t.Fatal(e)
	}
	s, e := OpenState(directory)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	w := newWorld(t, s)
	return s, w, Runner{Factory: w.factory, PollInterval: time.Millisecond}
}
func runRecovery(t *testing.T, r Runner, a Account, s *State, apply bool) Object {
	t.Helper()
	o, e := r.Recover(context.Background(), a, s, apply)
	if e != nil {
		t.Fatal(e)
	}
	return o
}
func hasCall(w *testWorld, suffix string) bool {
	for _, c := range w.calls {
		if strings.HasSuffix(c, suffix) {
			return true
		}
	}
	return false
}
func TestRecoveryReadOnlyAndNotBefore(t *testing.T) {
	for _, mode := range []string{"inspection", "waiting"} {
		t.Run(mode, func(t *testing.T) {
			s, w, r := setupRecovery(t)
			a := testAccount()
			if mode == "waiting" {
				a.NotBefore = "2099-01-01T00:00:00Z"
			}
			out := runRecovery(t, r, a, s, mode == "waiting")
			if out["state"] != mode {
				t.Fatal(out)
			}
			for _, op := range []string{":begin", ":acquire", ":pair_code", ":reconnect"} {
				if hasCall(w, op) {
					t.Fatal(w.calls)
				}
			}
			p, _ := s.Get(a.ID, "primary")
			if p.Phase != "" {
				t.Fatal(p)
			}
		})
	}
}
func TestRecoveryOrderingAndCleanup(t *testing.T) {
	s, w, r := setupRecovery(t)
	out := runRecovery(t, r, testAccount(), s, true)
	if out["state"] != "healthy" {
		t.Fatal(out)
	}
	var locks, pairs []string
	finish, closeHost := -1, -1
	for i, c := range w.calls {
		if strings.HasSuffix(c, ":acquire") {
			locks = append(locks, c)
		}
		if strings.HasSuffix(c, ":pair_code") {
			pairs = append(pairs, c)
		}
		if c == "phone:finish" {
			finish = i
		}
		if c == "primary:close" {
			closeHost = i
		}
	}
	if !reflect.DeepEqual(locks, []string{"backup:acquire", "primary:acquire"}) || !reflect.DeepEqual(pairs, []string{"primary:pair_code", "backup:pair_code"}) || finish < 0 || closeHost <= finish {
		t.Fatal(w.calls)
	}
	w.calls = nil
	if out = runRecovery(t, r, testAccount(), s, true); out["state"] != "healthy" || hasCall(w, ":begin") || hasCall(w, ":pair_code") {
		t.Fatal(out, w.calls)
	}
}
func TestRestrictionCooldownAndCleanup(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.fail["phone:prepare_pair"] = "restriction"
	out := runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "restriction" || number(out["retry_at"]) < float64(time.Now().Unix()+3500) || hasCall(w, ":pair_code") || !hasCall(w, ":finish") {
		t.Fatal(out, w.calls)
	}
	w.calls = nil
	out = runRecovery(t, r, testAccount(), s, true)
	if out["state"] != "waiting" || hasCall(w, ":begin") {
		t.Fatal(out, w.calls)
	}
}
func TestUncertainIntentSurvivesReopen(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.fail["primary:pair_code"] = "adapter_timeout"
	out := runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "adapter_timeout" {
		t.Fatal(out)
	}
	path := s.directory
	s.Close()
	reopened, e := OpenState(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	w.state = reopened
	w.calls = nil
	delete(w.fail, "primary:pair_code")
	out = runRecovery(t, r, testAccount(), reopened, true)
	if out["reason"] != "attempt_needs_review" || hasCall(w, ":pair_code") || hasCall(w, ":begin") {
		t.Fatal(out, w.calls)
	}
}
func TestPythonCompatibleStateAndLock(t *testing.T) {
	s, w, r := setupRecovery(t)
	_, e := s.db.Exec("INSERT INTO recovery VALUES ('sales','primary','needs_review','',0,0)")
	if e != nil {
		t.Fatal(e)
	}
	out := runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "attempt_needs_review" {
		t.Fatal(out)
	}
	l, e := acquireLock(filepath.Join(s.directory, "sales.lock"))
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	_, e = r.Recover(context.Background(), testAccount(), s, true)
	var b *Blocked
	if !errors.As(e, &b) || b.Reason != "lock_busy" {
		t.Fatal(e)
	}
	if hasCall(w, ":pair_code") {
		t.Fatal(w.calls)
	}
}
func TestSavedCredentialsReconnectFirst(t *testing.T) {
	for _, works := range []bool{false, true} {
		t.Run(map[bool]string{false: "blocked", true: "reconnected"}[works], func(t *testing.T) {
			s, w, r := setupRecovery(t)
			w.snapshots["backup"]["saved_sessions"] = 1
			w.snapshots["primary"] = snapshot(true, testPhone+":4@s.whatsapp.net")
			w.reconnect = works
			out := runRecovery(t, r, testAccount(), s, true)
			expected := "credentials_present"
			if works {
				expected = ""
			}
			if stringValue(out["reason"]) != expected || !hasCall(w, ":reconnect") || hasCall(w, ":pair_code") || hasCall(w, ":begin") {
				t.Fatal(out, w.calls)
			}
		})
	}
}
func TestRecoveryStopsOnGuards(t *testing.T) {
	for _, reason := range []string{"policy_failed", "identity_mismatch", "unknown_screen", "owner_verification", "slots_full", "ambiguous_device", "phone_busy", "profile_mismatch", "active_calls", "ownership_changed"} {
		t.Run(reason, func(t *testing.T) {
			s, w, r := setupRecovery(t)
			if reason == "policy_failed" {
				w.snapshots["backup"]["policy_ok"] = false
			} else if reason == "active_calls" || reason == "ownership_changed" {
				w.fail["backup:acquire"] = reason
			} else {
				w.fail["phone:prepare_pair"] = reason
			}
			out := runRecovery(t, r, testAccount(), s, true)
			if out["reason"] != reason || hasCall(w, ":pair_code") {
				t.Fatal(out, w.calls)
			}
		})
	}
}
func TestRequiresAffirmativePhoneIdentityAndReadiness(t *testing.T) {
	for _, mode := range []string{"identity", "ready"} {
		t.Run(mode, func(t *testing.T) {
			s, w, r := setupRecovery(t)
			if mode == "identity" {
				w.beginVerified = false
			} else {
				w.ready = false
			}
			out := runRecovery(t, r, testAccount(), s, true)
			if out["state"] != "blocked" || hasCall(w, ":pair_code") || !hasCall(w, ":finish") {
				t.Fatal(out, w.calls)
			}
		})
	}
}
func TestCleanupFailureBlocksAndKeepsCooldown(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.fail["phone:prepare_pair"] = "restriction"
	w.fail["phone:finish"] = "cleanup_failed"
	out := runRecovery(t, r, testAccount(), s, true)
	p, _ := s.Get("sales", "phone")
	if out["reason"] != "cleanup_failed" || p.RetryAt < float64(time.Now().Unix()+3500) {
		t.Fatal(out, p)
	}
}
func TestDuplicateJIDsRejected(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.duplicate = true
	out := runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "identity_mismatch" {
		t.Fatal(out)
	}
	w.calls = nil
	_, e := r.Recover(context.Background(), testAccount(), s, true)
	if e == nil || hasCall(w, ":begin") {
		t.Fatal(e, w.calls)
	}
}
func TestStateWriteFailureCannotRequestCode(t *testing.T) {
	s, w, r := setupRecovery(t)
	s.db.Close()
	_, e := r.Recover(context.Background(), testAccount(), s, true)
	if e == nil || hasCall(w, ":pair_code") {
		t.Fatal(e, w.calls)
	}
}
func TestPartialRecoveryDoesNotMintAgainOnRestart(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.fail["backup:pair_code"] = "adapter_timeout"
	out := runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "adapter_timeout" || !Healthy(w.snapshots["primary"]) {
		t.Fatal(out)
	}
	w.calls = nil
	out = runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "attempt_needs_review" || hasCall(w, ":pair_code") {
		t.Fatal(out, w.calls)
	}
}
func TestConfigRejectsAmbiguity(t *testing.T) {
	a := testAccount()
	for _, change := range []func(*Config){func(c *Config) { c.Accounts = append(c.Accounts, a) }, func(c *Config) { c.Accounts[0].NotBefore = "2099-01-01T00:00:00" }, func(c *Config) { c.Accounts[0].Phone = "+15551234567" }, func(c *Config) { c.Accounts[0].Backends[0].ID = "phone" }, func(c *Config) { c.Accounts[0].PhoneAdapter.Command = nil }} {
		c := Config{SchemaVersion: 1, Accounts: []Account{testAccount()}}
		change(&c)
		if c.Validate() == nil {
			t.Fatal(c)
		}
	}
}
func TestIncompleteSnapshotDoesNotProveCredentialAbsence(t *testing.T) {
	s, w, r := setupRecovery(t)
	delete(w.snapshots["backup"], "saved_sessions")
	_, e := r.Recover(context.Background(), testAccount(), s, true)
	b, ok := e.(*Blocked)
	if !ok || b.Reason != "adapter_protocol_error" || hasCall(w, ":pair_code") || hasCall(w, ":begin") {
		t.Fatal(e, w.calls)
	}
}
func TestHealthySessionsCannotHidePriorCleanupFailure(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.fail["phone:finish"] = "cleanup_failed"
	out := runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "cleanup_failed" {
		t.Fatal(out)
	}
	w.calls = nil
	delete(w.fail, "phone:finish")
	out = runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "cleanup_failed" || hasCall(w, ":begin") || hasCall(w, ":pair_code") {
		t.Fatal(out, w.calls)
	}
}

func TestMultiHostAccountScopedAdapterStopsBeforeMutations(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(map[bool]string{false: "logged out", true: "connected"}[connected], func(t *testing.T) {
			s, w, r := setupRecovery(t)
			w.snapshots["backup"] = snapshot(connected, "")
			w.snapshots["backup"]["identity_scope"] = "account"
			if connected {
				w.snapshots["backup"]["companion_jid"] = testPhone + "@s.whatsapp.net"
			}
			_, err := r.Recover(context.Background(), testAccount(), s, true)
			b, ok := err.(*Blocked)
			if !ok || b.Reason != "companion_identity_unavailable" {
				t.Fatal(err)
			}
			for _, op := range []string{":acquire", ":reconnect", ":begin", ":pair_code"} {
				if hasCall(w, op) {
					t.Fatal(w.calls)
				}
			}
		})
	}
}
func TestMultiHostNormalizedJIDIsNotIndependenceProof(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.snapshots["primary"] = snapshot(true, testPhone+"@s.whatsapp.net")
	w.snapshots["backup"] = snapshot(true, testPhone+":2@s.whatsapp.net")
	_, err := r.Recover(context.Background(), testAccount(), s, true)
	b, ok := err.(*Blocked)
	if !ok || b.Reason != "companion_identity_unavailable" {
		t.Fatal(err)
	}
	if hasCall(w, ":begin") || hasCall(w, ":pair_code") {
		t.Fatal(w.calls)
	}
}
func TestSingleHostHTTPAccountIdentityRemainsSupported(t *testing.T) {
	s, w, r := setupRecovery(t)
	a := testAccount()
	a.Backends = a.Backends[:1]
	w.snapshots["primary"] = snapshot(true, testPhone+"@s.whatsapp.net")
	w.snapshots["primary"]["identity_scope"] = "account"
	out := runRecovery(t, r, a, s, true)
	if out["state"] != "healthy" || hasCall(w, ":begin") {
		t.Fatal(out, w.calls)
	}
}

func TestInvalidAuthenticatedIdentityKeepsIntentAcrossRestart(t *testing.T) {
	for _, jid := range []string{"", testPhone + "@s.whatsapp.net"} {
		t.Run(jid, func(t *testing.T) {
			s, w, r := setupRecovery(t)
			w.postPair = map[string]Object{"primary": snapshot(true, jid)}
			out := runRecovery(t, r, testAccount(), s, true)
			if out["state"] != "blocked" {
				t.Fatal(out)
			}
			phase, err := s.Get("sales", "primary")
			if err != nil || phase.Phase != "pairing_started" {
				t.Fatal("unverified intent was cleared", phase, err)
			}
			w.snapshots["primary"] = snapshot(false, "")
			w.calls = nil
			out = runRecovery(t, r, testAccount(), s, true)
			if out["reason"] != "attempt_needs_review" || hasCall(w, ":pair_code") {
				t.Fatal(out, w.calls)
			}
		})
	}
}
func TestDuplicateAuthenticatedIdentitiesKeepBothIntents(t *testing.T) {
	s, w, r := setupRecovery(t)
	w.duplicate = true
	out := runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "identity_mismatch" {
		t.Fatal(out)
	}
	for _, backend := range []string{"primary", "backup"} {
		phase, err := s.Get("sales", backend)
		if err != nil || phase.Phase != "pairing_started" {
			t.Fatal(backend, phase, err)
		}
		w.snapshots[backend] = snapshot(false, "")
	}
	w.calls = nil
	out = runRecovery(t, r, testAccount(), s, true)
	if out["reason"] != "attempt_needs_review" || hasCall(w, ":pair_code") {
		t.Fatal(out, w.calls)
	}
}
func TestVerifiedAuthenticatedIdentitiesResolveOldIntentsWithoutPairing(t *testing.T) {
	s, w, r := setupRecovery(t)
	for i, backend := range []string{"primary", "backup"} {
		if err := s.Put("sales", backend, "pairing_started", "", 0); err != nil {
			t.Fatal(err)
		}
		w.snapshots[backend] = snapshot(true, fmt.Sprintf("%s:%d@s.whatsapp.net", testPhone, i+1))
	}
	out := runRecovery(t, r, testAccount(), s, true)
	if out["state"] != "healthy" || hasCall(w, ":pair_code") || hasCall(w, ":begin") {
		t.Fatal(out, w.calls)
	}
	for _, backend := range []string{"primary", "backup", "phone"} {
		p, err := s.Get("sales", backend)
		if err != nil || p.Phase != "healthy" {
			t.Fatal(p, err)
		}
	}
}
func TestRelativeStateDirectoryUsesSameDatabaseAndLock(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := OpenState("nested/state")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !filepath.IsAbs(s.directory) {
		t.Fatal(s.directory)
	}
	if err = s.Put("sales", "primary", "pairing_started", "", 0); err != nil {
		t.Fatal(err)
	}
	other, err := OpenState(s.directory)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	p, err := other.Get("sales", "primary")
	if err != nil || p.Phase != "pairing_started" {
		t.Fatal(p, err)
	}
	lock, err := acquireLock(filepath.Join(s.directory, "sales.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	_, err = (Runner{}).Recover(context.Background(), testAccount(), other, true)
	b, ok := err.(*Blocked)
	if !ok || b.Reason != "lock_busy" {
		t.Fatal(err)
	}
}
func TestConfigRejectsUnknownAndTrailingFields(t *testing.T) {
	c := Config{SchemaVersion: 1, Accounts: []Account{testAccount()}}
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"unknown": bytes.Replace(raw, []byte("\"verify_seconds\""), []byte("\"verify_second\""), 1), "trailing": append(append([]byte(nil), raw...), []byte(" {}")...)} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err == nil {
				t.Fatal("accepted invalid configuration")
			}
		})
	}
}
