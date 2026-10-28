package companion

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"sync"
	"time"
)

type Object = map[string]any
type Blocked struct {
	Reason     string
	RetryAfter int
}

func (b *Blocked) Error() string { return b.Reason }
func block(reason string) error  { return &Blocked{Reason: reason} }

var reasons = map[string]bool{}

func init() {
	for _, r := range []string{"adapter_timeout", "adapter_disconnected", "adapter_failed", "adapter_protocol_error", "adapter_response_too_large", "restriction", "owner_verification", "unknown_screen", "identity_mismatch", "companion_identity_unavailable", "phone_busy", "phone_unavailable", "profile_mismatch", "slots_full", "ambiguous_device", "credentials_present", "active_calls", "ownership_changed", "backend_unavailable", "pairing_unconfirmed", "cleanup_failed", "invalid_request", "lock_busy", "not_before", "attempt_needs_review", "policy_failed"} {
		reasons[r] = true
	}
}

type Adapter interface {
	Call(context.Context, string, Object) (Object, error)
	Close() error
}
type Factory func([]string) (Adapter, error)
type wireReply struct {
	OK         bool   `json:"ok"`
	Result     Object `json:"result"`
	Reason     string `json:"reason,omitempty"`
	RetryAfter int    `json:"retry_after,omitempty"`
}

// ProcessAdapter uses pipes only. Adapter prose and stderr never enter output.
type ProcessAdapter struct {
	cmd       *exec.Cmd
	in        io.WriteCloser
	out       io.ReadCloser
	reader    *bufio.Reader
	mu        sync.Mutex
	closeOnce sync.Once
}

func NewProcess(argv []string) (Adapter, error) {
	if !validCommand(argv) {
		return nil, block("invalid_request")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, block("adapter_failed")
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		in.Close()
		return nil, block("adapter_failed")
	}
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		in.Close()
		out.Close()
		return nil, block("adapter_failed")
	}
	return &ProcessAdapter{cmd: cmd, in: in, out: out, reader: bufio.NewReaderSize(out, 65537)}, nil
}
func (p *ProcessAdapter) Call(ctx context.Context, op string, values Object) (Object, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	request := Object{"op": op}
	for k, v := range values {
		if k != "op" {
			request[k] = v
		}
	}
	raw, e := json.Marshal(request)
	if e != nil {
		return nil, block("adapter_protocol_error")
	}
	raw = append(raw, '\n')
	type response struct {
		value Object
		err   error
	}
	done := make(chan response, 1)
	go func() {
		if _, err := p.in.Write(raw); err != nil {
			done <- response{err: block("adapter_disconnected")}
			return
		}
		line, err := p.reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull || len(line) > 65536 {
			done <- response{err: block("adapter_response_too_large")}
			return
		}
		if err != nil {
			done <- response{err: block("adapter_disconnected")}
			return
		}
		var r wireReply
		if json.Unmarshal(line, &r) != nil {
			done <- response{err: block("adapter_protocol_error")}
			return
		}
		if !r.OK {
			if !reasons[r.Reason] {
				r.Reason = "adapter_failed"
			}
			r.RetryAfter = max(0, min(86400, r.RetryAfter))
			done <- response{err: &Blocked{r.Reason, r.RetryAfter}}
			return
		}
		if r.Result == nil {
			r.Result = Object{}
		}
		done <- response{value: r.Result}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		p.in.Close()
		p.out.Close()
		_ = p.cmd.Process.Kill()
		<-done
		return nil, block("adapter_timeout")
	}
}
func (p *ProcessAdapter) Close() error {
	p.closeOnce.Do(func() {
		p.in.Close()
		done := make(chan error, 1)
		go func() { done <- p.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			_ = p.cmd.Process.Kill()
			<-done
		}
		p.out.Close()
	})
	return nil
}

// Serve keeps a single handler alive until EOF so reservations span all requests.
func Serve(in io.Reader, out io.Writer, handler Adapter) error {
	defer handler.Close()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 16385)
	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		var req Object
		reply := wireReply{}
		if len(scanner.Bytes()) > 16384 || json.Unmarshal(scanner.Bytes(), &req) != nil || req == nil {
			reply.Reason = "invalid_request"
		} else {
			op, _ := req["op"].(string)
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
			result, err := handler.Call(ctx, op, req)
			cancel()
			if err == nil {
				reply.OK = true
				reply.Result = result
			} else if b, ok := err.(*Blocked); ok && reasons[b.Reason] {
				reply.Reason = b.Reason
				reply.RetryAfter = b.RetryAfter
			} else {
				reply.Reason = "adapter_failed"
			}
		}
		if err := encoder.Encode(reply); err != nil {
			return err
		}
	}
	return scanner.Err()
}
