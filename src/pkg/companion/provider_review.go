package companion

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ReviewScreen classifies the official app without treating an appeal as pairing.
// In particular, a pairing cooldown never delays the first appeal request.
func ReviewScreen(xml, pkg string) (string, error) {
	s, err := parseRawScreen(xml, pkg)
	if err != nil {
		return "", err
	}
	words := s.Words
	for _, phrase := range []string{"verify your account", "verify account", "verify your phone", "verification code", "registration code", "6-digit", "enter your pin", "two-step verification", "fingerprint", "enter your password"} {
		if strings.Contains(words, phrase) {
			return "verification_required", nil
		}
	}
	for _, phrase := range []string{"account in review", "your account is being reviewed", "we're reviewing your account", "we’re reviewing your account"} {
		if strings.Contains(words, phrase) {
			return "review_pending", nil
		}
	}
	if strings.Contains(words, "this account can now use whatsapp") {
		return "approved", nil
	}
	banned := strings.Contains(words, "this account can't use whatsapp") || strings.Contains(words, "this account can’t use whatsapp") || strings.Contains(words, "this account can no longer use whatsapp")
	restricted := strings.Contains(words, "your account is restricted") || strings.Contains(words, "temporarily banned")
	if banned || restricted {
		if _, _, e := s.Unique("REQUEST REVIEW"); e == nil {
			return "review_available", nil
		}
		if banned {
			return "banned_no_review", nil
		}
	}
	if restricted {
		return "temporary_restriction", nil
	}
	if s.Title("Chats") || s.Title("Linked devices") || s.Title("Settings") {
		return "healthy", nil
	}
	return "unknown_screen", nil
}

// Review checks/submits only an account review. It never requests OTPs, pairs,
// resends messages or changes server credentials/ownership. The adapter binds
// the enrolled primary phone, Android user and app to the configured account.
func (r Runner) Review(ctx context.Context, a Account, state *State, apply bool) (out Object, err error) {
	if e := (Config{SchemaVersion: 1, Accounts: []Account{a}}).Validate(); e != nil {
		return nil, e
	}
	lock, e := acquireLock(filepath.Join(state.directory, a.ID+".lock"))
	if e != nil {
		return nil, e
	}
	defer lock.Close()
	if !a.AutoAppeal {
		return Object{"account": a.ID, "state": "disabled"}, nil
	}
	factory := r.Factory
	if factory == nil {
		factory = NewProcess
	}
	// A fully connected pair needs no phone access. Failed reads do not prevent
	// examining the primary phone for a provider ban.
	healthy := true
	for _, backend := range a.Backends {
		adapter, e := factory(backend.Command)
		if e != nil {
			healthy = false
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		snapshot, e := adapter.Call(callCtx, "inspect", Object{"device": a.ID, "phone": a.Phone})
		cancel()
		closeErr := adapter.Close()
		if e != nil || closeErr != nil || !Healthy(snapshot) {
			healthy = false
		}
	}
	previous, e := state.Get(a.ID, "provider-review")
	if e != nil {
		return nil, e
	}
	attempt, e := state.Get(a.ID, "provider-review-attempt")
	if e != nil {
		return nil, e
	}
	if healthy {
		if apply {
			if e = state.Put(a.ID, "provider-review-attempt", "healthy", "", 0); e != nil {
				return nil, e
			}
			if e = state.Put(a.ID, "provider-review", "healthy", "", 0); e != nil {
				return nil, e
			}
		}
		return Object{"account": a.ID, "state": "healthy"}, nil
	}
	if previous.Phase == "review_pending" && float64(time.Now().Unix()) < previous.RetryAt {
		return Object{"account": a.ID, "state": "review_pending", "next_check": previous.RetryAt}, nil
	}
	phone, e := factory(a.PhoneAdapter.Command)
	if e != nil {
		return nil, e
	}
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		_, cleanErr := phone.Call(cleanCtx, "finish", Object{})
		cancel()
		closeErr := phone.Close()
		if cleanErr != nil || closeErr != nil {
			out = Object{"account": a.ID, "state": "blocked", "reason": "cleanup_failed"}
			err = block("cleanup_failed")
		}
	}()
	call := func(op string, values Object) (Object, error) {
		c, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		return phone.Call(c, op, values)
	}
	result, e := call("review_begin", Object{"account": Object{"id": a.ID, "phone": a.Phone, "display_name": a.DisplayName}, "config": a.PhoneAdapter.Config})
	if e != nil {
		return nil, e
	}
	if result["identity_verified"] != true {
		return nil, block("identity_mismatch")
	}
	pkg := stringValue(a.PhoneAdapter.Config["package"])
	if pkg == "" {
		pkg = "com.whatsapp.w4b"
	}
	status, e := ReviewScreen(stringValue(result["xml"]), pkg)
	if e != nil {
		return nil, e
	}
	record := func(status string) (Object, error) {
		if apply {
			if status == "healthy" || status == "review_pending" || status == "approved" || status == "verification_required" {
				if e := state.Put(a.ID, "provider-review-attempt", status, "", 0); e != nil {
					return nil, e
				}
			}
			retry := float64(0)
			if status == "review_pending" {
				retry = float64(time.Now().Unix() + 300)
			}
			if e := state.Put(a.ID, "provider-review", status, "", retry); e != nil {
				return nil, e
			}
		}
		out := Object{"account": a.ID, "state": status}
		if status == "verification_required" {
			words := strings.ToLower(stringValue(result["xml"]))
			if strings.Contains(words, "before you can request a review") {
				out["verification_stage"] = "before_review"
				out["appeal_confirmed"] = false
			} else if strings.Contains(words, "this account can now use whatsapp") {
				out["verification_stage"] = "after_approval"
				out["appeal_confirmed"] = true
				out["review_approved"] = true
			}
		}
		if status == "verification_required" || status == "unknown_screen" || status == "banned_no_review" {
			out["needs_owner"] = true
		}
		return out, nil
	}
	if status != "review_available" {
		return record(status)
	}
	if !apply {
		return Object{"account": a.ID, "state": status, "would_submit": true}, nil
	}
	// An interrupted/uncertain submission remains durable across new processes.
	// Observe manual/pending reviews, but never press REQUEST REVIEW twice in an
	// unresolved ban episode, including after an authentication interruption.
	switch attempt.Phase {
	case "submission_started", "review_submitted", "review_pending", "verification_required", "attempt_needs_review", "approved":
		return Object{"account": a.ID, "state": "attempt_needs_review", "needs_owner": true}, nil
	}
	if e = state.Put(a.ID, "provider-review-attempt", "submission_started", "", 0); e != nil {
		return nil, e
	}
	if e = state.Put(a.ID, "provider-review", "submission_started", "", 0); e != nil {
		return nil, e
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(stringValue(result["xml"]))))
	result, e = call("review_request", Object{"screen_sha256": hash})
	if e != nil {
		return nil, e
	}
	status, e = ReviewScreen(stringValue(result["xml"]), pkg)
	if e != nil {
		return nil, e
	}
	if status == "review_available" {
		return Object{"account": a.ID, "state": "attempt_needs_review", "needs_owner": true}, nil
	}
	out, e = record(status)
	if e != nil {
		return nil, e
	}
	if status == "review_pending" {
		out["submitted_now"] = true
	}
	return out, e
}
