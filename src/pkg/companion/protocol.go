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

// ProcessAdapter uses pipes only. Cancellation poisons a call's framing without
// killing the process: the runner must finish phone cleanup before closing hosts.
type ProcessAdapter struct {
	cmd        *exec.Cmd
	in         io.WriteCloser
	out        io.ReadCloser
	reader     *bufio.Reader
	mu         sync.Mutex
	pending    chan response
	broken     bool
	closeDone  chan struct{}
	closeErr   error
	closeGrace time.Duration
}
type response struct {
	value  Object
	err    error
	broken bool
}

func NewProcess(argv []string) (Adapter, error) {
	if !validCommand(argv) {
		return nil, block("invalid_request")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	isolateProcess(cmd)
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
	return &ProcessAdapter{cmd: cmd, in: in, out: out, reader: bufio.NewReaderSize(out, 65537), closeGrace: 20 * time.Second}, nil
}
func (p *ProcessAdapter) Call(ctx context.Context, op string, values Object) (Object, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken {
		if op != "finish" {
			return nil, block("adapter_disconnected")
		}
		// EOF lets a cooperative adapter cancel its in-flight operation and run Close.
		// Never issue a new request into a stream whose reply framing is uncertain.
		p.startClose()
		if e := p.awaitClose(ctx); e != nil {
			return nil, block("cleanup_failed")
		}
		return Object{}, nil
	}
	if p.closeDone != nil {
		return nil, block("adapter_disconnected")
	}
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
	done := make(chan response, 1)
	p.pending = done
	go func() {
		if _, err := p.in.Write(raw); err != nil {
			done <- response{err: block("adapter_disconnected"), broken: true}
			return
		}
		line, err := p.reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull || len(line) > 65536 {
			done <- response{err: block("adapter_response_too_large"), broken: true}
			return
		}
		if err != nil {
			done <- response{err: block("adapter_disconnected"), broken: true}
			return
		}
		var r wireReply
		if json.Unmarshal(line, &r) != nil {
			done <- response{err: block("adapter_protocol_error"), broken: true}
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
		p.pending = nil
		p.broken = r.broken
		return r.value, r.err
	case <-ctx.Done():
		p.broken = true
		return nil, block("adapter_timeout")
	}
}

// startClose is called with mu held. Only this point closes stdin/releases a
// server adapter's lock. An outstanding reader is drained before discarding any
// remaining stdout, so oversized or late replies cannot become another reply.
func (p *ProcessAdapter) startClose() {
	if p.closeDone != nil {
		return
	}
	p.closeDone = make(chan struct{})
	p.in.Close()
	pending := p.pending
	go func() {
		if pending != nil {
			<-pending
		}
		_, _ = io.Copy(io.Discard, p.reader)
	}()
	go func() { p.closeErr = p.cmd.Wait(); p.out.Close(); close(p.closeDone) }()
}
func (p *ProcessAdapter) awaitClose(ctx context.Context) error {
	select {
	case <-p.closeDone:
		if p.closeErr != nil {
			return block("adapter_failed")
		}
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		<-p.closeDone
		return block("adapter_timeout")
	}
}
func (p *ProcessAdapter) Close() error {
	p.mu.Lock()
	p.startClose()
	grace := p.closeGrace
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	return p.awaitClose(ctx)
}

// Serve keeps a single handler alive until EOF so reservations span all requests.
func Serve(in io.Reader, out io.Writer, handler Adapter) error {
	return ServeContext(context.Background(), in, out, handler)
}

// ServeContext detects EOF/signals while a request is running, cancels that
// operation and serially restores/releases the handler before returning. Finish
// and Close have independent cleanup deadlines, not the cancelled request context.
func ServeContext(ctx context.Context, in io.Reader, out io.Writer, handler Adapter) (err error) {
	defer func() {
		if closeErr := handler.Close(); closeErr != nil {
			err = closeErr
		}
	}()
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	requests := make(chan []byte)
	readDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 4096), 16385)
		defer close(requests)
		defer cancel()
		for scanner.Scan() {
			raw := append([]byte(nil), scanner.Bytes()...)
			select {
			case requests <- raw:
			case <-opCtx.Done():
				readDone <- nil
				return
			}
		}
		readDone <- scanner.Err()
	}()
	go func() {
		<-opCtx.Done()
		if closer, ok := in.(io.Closer); ok {
			_ = closer.Close()
		}
	}()
	encoder := json.NewEncoder(out)
	for raw := range requests {
		if ctx.Err() != nil {
			break
		}
		var req Object
		reply := wireReply{}
		if len(raw) > 16384 || json.Unmarshal(raw, &req) != nil || req == nil {
			reply.Reason = "invalid_request"
		} else {
			op, _ := req["op"].(string)
			requestCtx, requestCancel := context.WithTimeout(opCtx, 180*time.Second)
			if op == "finish" {
				requestCancel()
				requestCtx, requestCancel = context.WithTimeout(context.Background(), 60*time.Second)
			}
			result, callErr := handler.Call(requestCtx, op, req)
			requestCancel()
			if callErr == nil {
				reply.OK = true
				reply.Result = result
			} else if b, ok := callErr.(*Blocked); ok && reasons[b.Reason] {
				reply.Reason = b.Reason
				reply.RetryAfter = b.RetryAfter
			} else {
				reply.Reason = "adapter_failed"
			}
		}
		if e := encoder.Encode(reply); e != nil {
			return e
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	return <-readDone
}
