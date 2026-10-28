package companion

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
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
