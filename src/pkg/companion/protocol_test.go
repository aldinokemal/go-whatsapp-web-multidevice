package companion

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdapterHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "companion-helper" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "timeout":
		time.Sleep(time.Minute)
	case "cooperative", "cooperative-large", "cooperative-signal":
		helper := &cleanupHelper{large: mode == "cooperative-large"}
		ctx := context.Background()
		if mode == "cooperative-signal" {
			var stop context.CancelFunc
			ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
			defer stop()
		}
		if ServeContext(ctx, os.Stdin, os.Stdout, helper) != nil {
			os.Exit(2)
		}
	case "large":
		os.Stdout.Write([]byte(strings.Repeat("x", 65537) + "\n"))
	case "secret":
		os.Stdout.Write([]byte(`{"ok":false,"reason":"PRIVATE CODE OR CUSTOMER DATA"}` + "\n"))
	case "retry":
		os.Stdout.Write([]byte(`{"ok":false,"reason":"restriction","retry_after":999999}` + "\n"))
	case "malformed":
		os.Stdout.Write([]byte("not json\n"))
	case "eof":
	case "normal":
		scanner := json.NewDecoder(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)
		for {
			var request Object
			if scanner.Decode(&request) != nil {
				break
			}
			encoder.Encode(wireReply{OK: true, Result: Object{"operation": request["op"]}})
		}
	}
	os.Exit(0)
}
func TestProcessAdapterBoundaries(t *testing.T) {
	for mode, reason := range map[string]string{"timeout": "adapter_timeout", "large": "adapter_response_too_large", "secret": "adapter_failed", "retry": "restriction", "malformed": "adapter_protocol_error", "eof": "adapter_disconnected", "normal": ""} {
		t.Run(mode, func(t *testing.T) {
			a, e := NewProcess([]string{os.Args[0], "-test.run=TestAdapterHelper", "--", "companion-helper", mode})
			if e != nil {
				t.Fatal(e)
			}
			a.(*ProcessAdapter).closeGrace = 50 * time.Millisecond
			defer a.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if mode == "timeout" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
			}
			defer cancel()
			result, e := a.Call(ctx, "inspect", Object{})
			if reason == "" {
				if e != nil || result["operation"] != "inspect" {
					t.Fatal(result, e)
				}
				result, e = a.Call(ctx, "finish", Object{})
				if e != nil || result["operation"] != "finish" {
					t.Fatal(result, e)
				}
			} else {
				b, ok := e.(*Blocked)
				if !ok || b.Reason != reason {
					t.Fatal(e)
				}
				if mode == "retry" && b.RetryAfter != 86400 {
					t.Fatal(b)
				}
			}
		})
	}
}

type serveAdapter struct{ closed bool }

func (a *serveAdapter) Call(_ context.Context, op string, _ Object) (Object, error) {
	if op == "bad" {
		return nil, block("owner_verification")
	}
	return Object{"op": op}, nil
}
func (a *serveAdapter) Close() error { a.closed = true; return nil }
func TestServeLifetimeAndInvalidRequests(t *testing.T) {
	a := &serveAdapter{}
	var out bytes.Buffer
	e := Serve(strings.NewReader("{\"op\":\"begin\"}\n[]\n{\"op\":\"bad\"}\n{\"op\":\"finish\"}\n"), &out, a)
	if e != nil || !a.closed {
		t.Fatal(e)
	}
	var replies []wireReply
	d := json.NewDecoder(&out)
	for {
		var r wireReply
		if d.Decode(&r) != nil {
			break
		}
		replies = append(replies, r)
	}
	if len(replies) != 4 || !replies[0].OK || replies[1].Reason != "invalid_request" || replies[2].Reason != "owner_verification" || !replies[3].OK {
		t.Fatal(replies)
	}
}

type cleanupHelper struct {
	large bool
	lock  *fileLock
}

func (h *cleanupHelper) Call(ctx context.Context, op string, _ Object) (Object, error) {
	marker := os.Getenv("GOWA_COMPANION_TEST_EVIDENCE")
	var err error
	h.lock, err = acquireLock(filepath.Join(marker, "host.lock"))
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(marker, "started"), []byte("ready"), 0600); err != nil {
		return nil, err
	}
	if h.large {
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", 65537) + "\n"))
		return Object{}, nil
	}
	<-ctx.Done()
	return nil, block("adapter_timeout")
}
func (h *cleanupHelper) Close() error {
	root := os.Getenv("GOWA_COMPANION_TEST_EVIDENCE")
	err := os.WriteFile(filepath.Join(root, "restored"), []byte("done"), 0600)
	if h.lock != nil {
		h.lock.Close()
	}
	return err
}
func TestInterruptedAdapterKeepsLockUntilGracefulCleanup(t *testing.T) {
	for _, mode := range []string{"cooperative", "cooperative-large"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("GOWA_COMPANION_TEST_EVIDENCE", root)
			adapter, err := NewProcess([]string{os.Args[0], "-test.run=TestAdapterHelper", "--", "companion-helper", mode})
			if err != nil {
				t.Fatal(err)
			}
			defer adapter.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			called := make(chan error, 1)
			go func() { _, err := adapter.Call(ctx, "enter_code", Object{}); called <- err }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err = os.Stat(filepath.Join(root, "started")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("adapter did not start")
				}
				time.Sleep(time.Millisecond)
			}
			if mode == "cooperative" {
				cancel()
			}
			select {
			case err = <-called:
				if err == nil {
					t.Fatal("missing blocked result")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("call was not bounded")
			}
			// Host locks must still cover the phone cleanup, even after cancellation or
			// an oversized reply. No second request may consume stale reply bytes.
			if l, err := acquireLock(filepath.Join(root, "host.lock")); err == nil {
				l.Close()
				t.Fatal("host lock released before cleanup")
			}
			if _, err = adapter.Call(context.Background(), "inspect", Object{}); err == nil {
				t.Fatal("poisoned protocol reused")
			}
			cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelCleanup()
			if _, err = adapter.Call(cleanup, "finish", Object{}); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(root, "restored")); err != nil {
				t.Fatal("cleanup was skipped", err)
			}
			if err = adapter.Close(); err != nil {
				t.Fatal(err)
			}
			l, err := acquireLock(filepath.Join(root, "host.lock"))
			if err != nil {
				t.Fatal("host lock leaked", err)
			}
			l.Close()
		})
	}
}
func TestServeContextCancellationRestoresHandler(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWA_COMPANION_TEST_EVIDENCE", root)
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeContext(ctx, reader, io.Discard, &cleanupHelper{}) }()
	if _, err := writer.Write([]byte("{\"op\":\"enter_code\"}\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handler did not start")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not complete")
	}
	if _, err := os.Stat(filepath.Join(root, "restored")); err != nil {
		t.Fatal(err)
	}
}
