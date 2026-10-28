package companion

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

type Runner struct {
	Factory      Factory
	Now          func() time.Time
	PollInterval time.Duration
	CallTimeout  time.Duration
}

func Healthy(s Object) bool {
	return s["connected"] == true && s["logged_in"] == true && s["policy_ok"] == true
}
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
func (r Runner) Recover(ctx context.Context, a Account, state *State, apply bool) (out Object, err error) {
	if e := (Config{SchemaVersion: 1, Accounts: []Account{a}}).Validate(); e != nil {
		return nil, e
	}
	factory := r.Factory
	if factory == nil {
		factory = NewProcess
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	callTimeout := r.CallTimeout
	if callTimeout == 0 {
		callTimeout = 180 * time.Second
	}
	poll := r.PollInterval
	if poll == 0 {
		poll = 2 * time.Second
	}
	lock, e := acquireLock(filepath.Join(state.directory, a.ID+".lock"))
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	type backend struct {
		id       string
		adapter  Adapter
		snapshot Object
	}
	var backends []backend
	defer func() {
		for i := len(backends) - 1; i >= 0; i-- {
			backends[i].adapter.Close()
		}
	}()
	call := func(adapter Adapter, op string, values Object) (Object, error) {
		c, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()
		result, e := adapter.Call(c, op, values)
		if e == nil && op == "inspect" {
			e = validateSnapshot(result, a.Phone)
			if e == nil && len(a.Backends) > 1 && result["identity_scope"] == "account" {
				e = block("companion_identity_unavailable")
			}
		}
		return result, e
	}
	identity := Object{"device": a.ID, "phone": a.Phone}
	configs := append([]BackendConfig(nil), a.Backends...)
	sort.Slice(configs, func(i, j int) bool { return configs[i].ID < configs[j].ID })
	allHealthy := true
	for _, c := range configs {
		adapter, e := factory(c.Command)
		if e != nil {
			return nil, e
		}
		backends = append(backends, backend{id: c.ID, adapter: adapter})
		s, e := call(adapter, "inspect", identity)
		if e != nil {
			return nil, e
		}
		backends[len(backends)-1].snapshot = s
		allHealthy = allHealthy && Healthy(s)
	}
	ids := []string{}
	for _, b := range backends {
		ids = append(ids, b.id)
	}
	// Even an already-connected account must have independent, verified identities.
	verify := func(snapshots []Object) error {
		seen := map[string]bool{}
		fullJID := regexp.MustCompile(`^` + regexp.QuoteMeta(a.Phone) + `:[0-9]+@s\.whatsapp\.net$`)
		for _, s := range snapshots {
			if !Healthy(s) {
				return block("policy_failed")
			}
			jid, ok := s["companion_jid"].(string)
			if !ok || jid == "" || seen[jid] {
				return block("identity_mismatch")
			}
			if len(a.Backends) > 1 && !fullJID.MatchString(jid) {
				return block("companion_identity_unavailable")
			}
			seen[jid] = true
		}
		return nil
	}
	p, e := state.Get(a.ID, "phone")
	if e != nil {
		return nil, e
	}
	if apply && p.Reason == "cleanup_failed" {
		return Object{"account": a.ID, "state": "blocked", "reason": "cleanup_failed", "retry_at": p.RetryAt}, nil
	}
	if allHealthy {
		snapshots := []Object{}
		for _, b := range backends {
			snapshots = append(snapshots, b.snapshot)
		}
		if e = verify(snapshots); e != nil {
			return nil, e
		}
		if apply {
			for _, b := range backends {
				if e = state.Put(a.ID, b.id, "healthy", "", 0); e != nil {
					return nil, e
				}
			}
			if e = state.Put(a.ID, "phone", "healthy", "", 0); e != nil {
				return nil, e
			}
		}
		return Object{"account": a.ID, "state": "healthy", "backends": ids}, nil
	}
	if !apply {
		snapshots := []Object{}
		for _, b := range backends {
			s := Object{"id": b.id}
			for k, v := range b.snapshot {
				s[k] = v
			}
			snapshots = append(snapshots, s)
		}
		return Object{"account": a.ID, "state": "inspection", "backends": snapshots, "recovery": p, "not_before": a.NotBefore}, nil
	}
	notBefore, e := a.notBefore()
	if e != nil {
		return nil, e
	}
	retryAt := max(notBefore, p.RetryAt)
	if float64(now.Unix()) < retryAt {
		return Object{"account": a.ID, "state": "waiting", "reason": "not_before", "retry_at": retryAt}, nil
	}
	var phone Adapter
	// Cleanup runs before server adapters release their host locks, even on errors.
	defer func() {
		if phone != nil {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			_, cleanErr := phone.Call(cleanCtx, "finish", Object{})
			cancel()
			closeErr := phone.Close()
			if cleanErr != nil || closeErr != nil {
				out = Object{"account": a.ID, "state": "blocked", "reason": "cleanup_failed"}
				err = state.Put(a.ID, "phone", "blocked", "cleanup_failed", p.RetryAt)
			}
		}
	}()
	work := func() error {
		for _, b := range backends {
			if _, e := call(b.adapter, "acquire", identity); e != nil {
				return e
			}
		}
		missing := map[string]Adapter{}
		for _, b := range backends {
			s, e := call(b.adapter, "inspect", identity)
			if e != nil {
				return e
			}
			if !Healthy(s) && (s["logged_in"] == true || number(s["saved_sessions"]) > 0) {
				if _, e = call(b.adapter, "reconnect", identity); e != nil {
					return e
				}
				s, e = call(b.adapter, "inspect", identity)
				if e != nil {
					return e
				}
				if !Healthy(s) {
					return block("credentials_present")
				}
			}
			if Healthy(s) {
				if e = state.Put(a.ID, b.id, "healthy", "", 0); e != nil {
					return e
				}
				continue
			}
			if s["policy_ok"] != true {
				return block("policy_failed")
			}
			phase, e := state.Get(a.ID, b.id)
			if e != nil {
				return e
			}
			if phase.Phase == "pairing_started" || phase.Phase == "needs_review" {
				return block("attempt_needs_review")
			}
			missing[b.id] = b.adapter
		}
		if len(missing) > 0 {
			var e error
			phone, e = factory(a.PhoneAdapter.Command)
			if e != nil {
				return e
			}
			begin, e := call(phone, "begin", Object{"account": Object{"id": a.ID, "phone": a.Phone, "display_name": a.DisplayName}, "config": a.PhoneAdapter.Config})
			if e != nil {
				return e
			}
			if begin["identity_verified"] != true {
				return block("identity_mismatch")
			}
			for _, config := range a.Backends {
				adapter, ok := missing[config.ID]
				if !ok {
					continue
				}
				ready, e := call(phone, "prepare_pair", Object{"backend": config.ID})
				if e != nil {
					return e
				}
				if ready["ready"] != true {
					return block("unknown_screen")
				}
				if e = state.Put(a.ID, config.ID, "pairing_started", "", 0); e != nil {
					return e
				}
				result, e := call(adapter, "pair_code", identity)
				if e != nil {
					return e
				}
				code, _ := result["pair_code"].(string)
				if !pairingCode.MatchString(code) {
					return block("adapter_protocol_error")
				}
				if _, e = call(phone, "enter_code", Object{"code": code}); e != nil {
					return e
				}
				delete(result, "pair_code")
				code = ""
				seconds := a.VerifySeconds
				if seconds == 0 {
					seconds = 60
				}
				deadline := time.Now().Add(time.Duration(seconds) * time.Second)
				for {
					s, e := call(adapter, "inspect", identity)
					if e != nil {
						return e
					}
					if Healthy(s) {
						if e = state.Put(a.ID, config.ID, "healthy", "", 0); e != nil {
							return e
						}
						break
					}
					if time.Now().After(deadline) {
						return block("pairing_unconfirmed")
					}
					select {
					case <-ctx.Done():
						return block("pairing_unconfirmed")
					case <-time.After(poll):
					}
				}
			}
		}
		snapshots := []Object{}
		for _, b := range backends {
			s, e := call(b.adapter, "inspect", identity)
			if e != nil {
				return e
			}
			snapshots = append(snapshots, s)
		}
		if e := verify(snapshots); e != nil {
			return e
		}
		return state.Put(a.ID, "phone", "healthy", "", 0)
	}
	e = work()
	if e != nil {
		var b *Blocked
		if !errors.As(e, &b) {
			return nil, e
		}
		delay := b.RetryAfter
		if delay == 0 && b.Reason == "restriction" {
			delay = 21600
		}
		until := float64(0)
		if delay > 0 {
			until = float64(time.Now().Unix()) + float64(delay)
		}
		p.RetryAt = until
		if e = state.Put(a.ID, "phone", "blocked", b.Reason, until); e != nil {
			return nil, e
		}
		return Object{"account": a.ID, "state": "blocked", "reason": b.Reason, "retry_at": until}, nil
	}
	return Object{"account": a.ID, "state": "healthy", "backends": ids}, nil
}

// A missing field must not be interpreted as proof that credentials are absent.
func validateSnapshot(s Object, phone string) error {
	for _, key := range []string{"connected", "logged_in", "policy_ok"} {
		if _, ok := s[key].(bool); !ok {
			return block("adapter_protocol_error")
		}
	}
	saved, exists := s["saved_sessions"]
	if !exists {
		return block("adapter_protocol_error")
	}
	switch saved.(type) {
	case int, int64, float64:
	default:
		return block("adapter_protocol_error")
	}
	n := number(saved)
	if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		return block("adapter_protocol_error")
	}
	jid, ok := s["companion_jid"].(string)
	if !ok {
		return block("adapter_protocol_error")
	}
	if jid != "" && !regexp.MustCompile(`^`+regexp.QuoteMeta(phone)+`(?::[0-9]+)?@s\.whatsapp\.net$`).MatchString(jid) {
		return block("identity_mismatch")
	}
	return nil
}
