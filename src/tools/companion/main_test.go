package main

import (
	"context"
	"os"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/companion"
)

type accountAdapter struct {
	fail      bool
	onInspect func()
}

func (a *accountAdapter) Call(_ context.Context, op string, _ companion.Object) (companion.Object, error) {
	if op != "inspect" {
		return nil, &companion.Blocked{Reason: "invalid_request"}
	}
	if a.onInspect != nil {
		a.onInspect()
	}
	if a.fail {
		return nil, &companion.Blocked{Reason: "backend_unavailable"}
	}
	return companion.Object{"connected": true, "logged_in": true, "policy_ok": true, "saved_sessions": 1, "companion_jid": "15551234567:1@s.whatsapp.net"}, nil
}
func (a *accountAdapter) Close() error { return nil }
func TestAccountFailurePreservesResultsAndContinues(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := companion.OpenState(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	var accounts []companion.Account
	for _, name := range []string{"first", "broken", "last"} {
		accounts = append(accounts, companion.Account{ID: name, Phone: "15551234567", DisplayName: "Example", Backends: []companion.BackendConfig{{ID: "backend", Command: []string{name}}}, PhoneAdapter: companion.PhoneConfig{Command: []string{"must-not-start"}}})
	}
	runner := companion.Runner{Factory: func(command []string) (companion.Adapter, error) {
		if command[0] == "must-not-start" {
			t.Fatal("unexpected phone process")
		}
		return &accountAdapter{fail: command[0] == "broken"}, nil
	}}
	results, status := runAccounts(context.Background(), runner, accounts, state, "", false)
	if status != 2 || len(results) != 3 {
		t.Fatal(results, status)
	}
	for i, name := range []string{"first", "broken", "last"} {
		if results[i]["account"] != name {
			t.Fatal(results)
		}
	}
	if results[0]["state"] != "healthy" || results[1]["reason"] != "backend_unavailable" || results[2]["state"] != "healthy" {
		t.Fatal(results)
	}
}

func TestCancellationAfterHealthyResultFailsRun(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := companion.OpenState(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	var accounts []companion.Account
	for _, name := range []string{"first", "skipped"} {
		accounts = append(accounts, companion.Account{ID: name, Phone: "15551234567", DisplayName: "Example", Backends: []companion.BackendConfig{{ID: "backend", Command: []string{name}}}, PhoneAdapter: companion.PhoneConfig{Command: []string{"must-not-start"}}})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := companion.Runner{Factory: func(command []string) (companion.Adapter, error) {
		switch command[0] {
		case "first":
			// The signal arrives while the first account is being inspected,
			// after which its inspection still completes healthy.
			return &accountAdapter{onInspect: cancel}, nil
		case "skipped":
			t.Fatal("account after cancellation was started")
		}
		t.Fatal("unexpected phone process")
		return nil, nil
	}}
	results, status := runAccounts(ctx, runner, accounts, state, "", false)
	if status != 2 {
		t.Fatalf("status = %d after cancellation skipped an account, want 2; results %v", status, results)
	}
	if len(results) != 1 || results[0]["account"] != "first" {
		t.Fatal(results)
	}
}
