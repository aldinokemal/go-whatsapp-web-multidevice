package companion

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func availableReview() string {
	return screenXML(node("This account can't use WhatsApp", ""), node("REQUEST REVIEW", actionable))
}
func pendingReview() string { return screenXML(node("Account in review", "")) }
func TestReviewScreenStates(t *testing.T) {
	for _, tc := range []struct{ xml, state string }{
		{availableReview(), "review_available"}, {screenXML(node("Your account is restricted right now", ""), node("REQUEST REVIEW", actionable)), "review_available"}, {pendingReview(), "review_pending"},
		{screenXML(node("Verify your account", ""), node("Before you can request a review, we will need to send you an SMS to verify your account.", "")), "verification_required"},
		{screenXML(node("This account can now use WhatsApp", ""), node("VERIFY ACCOUNT", actionable)), "verification_required"},
		{screenXML(node("This account can now use WhatsApp", "")), "approved"},
		{screenXML(node("This account can't use WhatsApp", "")), "banned_no_review"},
		{screenXML(node("Temporarily banned", "")), "temporary_restriction"},
		{screenXML(node("Chats", "")), "healthy"},
		{screenXML(node("Enter the 6-digit code", "")), "verification_required"},
		{screenXML(node("Request review", "")), "unknown_screen"},
	} {
		t.Run(tc.state+fmt.Sprint(len(tc.xml)), func(t *testing.T) {
			s, e := ReviewScreen(tc.xml, testPackage)
			if e != nil || s != tc.state {
				t.Fatal(s, e)
			}
		})
	}
	if _, e := ReviewScreen(`<hierarchy><node package="another.app" text="Account in review" /></hierarchy>`, testPackage); e == nil {
		t.Fatal("accepted wrong app")
	}
}

type reviewWorld struct {
	state                         *State
	xml, after                    string
	taps, opens                   int
	healthy, binding, cleanupFail bool
	untapped                      bool
}
type reviewAdapter struct {
	w     *reviewWorld
	phone bool
}

func (a *reviewAdapter) Close() error { return nil }
func (a *reviewAdapter) Call(_ context.Context, op string, _ Object) (Object, error) {
	if !a.phone {
		return snapshot(a.w.healthy, testPhone+":1@s.whatsapp.net"), nil
	}
	switch op {
	case "review_begin":
		a.w.opens++
		return Object{"identity_verified": a.w.binding, "xml": a.w.xml}, nil
	case "review_request":
		intent, e := a.w.state.Get("sales", "provider-review-attempt")
		if e != nil || intent.Phase != "submission_started" {
			return nil, fmt.Errorf("tap without durable intent")
		}
		a.w.xml = a.w.after
		if a.w.untapped {
			return Object{"xml": a.w.xml, "tapped": false}, nil
		}
		a.w.taps++
		return Object{"xml": a.w.xml, "tapped": true}, nil
	case "finish":
		if a.w.cleanupFail {
			return nil, block("cleanup_failed")
		}
		return Object{}, nil
	}
	return nil, block("invalid_request")
}
func setupReview(t *testing.T) (*State, *reviewWorld, Runner, Account) {
	s, _, _ := setupRecovery(t)
	w := &reviewWorld{state: s, xml: availableReview(), after: pendingReview(), binding: true}
	r := Runner{Factory: func(argv []string) (Adapter, error) { return &reviewAdapter{w, argv[0] == "phone"}, nil }}
	a := testAccount()
	a.AutoAppeal = true
	return s, w, r, a
}
func TestReviewImmediateDespitePairingCooldown(t *testing.T) {
	s, w, r, a := setupReview(t)
	a.NotBefore = time.Now().Add(6 * time.Hour).Format(time.RFC3339)
	if e := s.Put(a.ID, "phone", "blocked", "restriction", float64(time.Now().Unix()+21600)); e != nil {
		t.Fatal(e)
	}
	out, e := r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "review_pending" || out["submitted_now"] != true || w.taps != 1 {
		t.Fatal(out, e, w.taps)
	}
	phone, _ := s.Get(a.ID, "phone")
	if phone.Reason != "restriction" {
		t.Fatal("changed pairing cooldown")
	}
	out, e = r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "review_pending" || w.taps != 1 {
		t.Fatal(out, e, w.taps)
	}
}
func TestReviewInterruptedOrUnknownOutcomeNeverRetaps(t *testing.T) {
	s, w, r, a := setupReview(t)
	w.after = screenXML(node("Something unfamiliar", ""))
	out, e := r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "unknown_screen" || w.taps != 1 {
		t.Fatal(out, e, w.taps)
	}
	w.xml = availableReview()
	out, e = r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "attempt_needs_review" || w.taps != 1 {
		t.Fatal(out, e, w.taps)
	}
}
func TestReviewVerificationEscalatesImmediatelyWithoutSecondRequest(t *testing.T) {
	s, w, r, a := setupReview(t)
	w.after = screenXML(node("Verify your account", ""), node("Before you can request a review, we will need to send you an SMS to verify your account.", ""))
	out, e := r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "verification_required" || out["needs_owner"] != true || out["verification_stage"] != "before_review" || w.taps != 1 {
		t.Fatal(out, e, w.taps)
	}
	w.xml = availableReview()
	out, e = r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "attempt_needs_review" || w.taps != 1 {
		t.Fatal(out, e, w.taps)
	}
}
func TestReviewAdoptsManualSubmissionAndResolvesAfterHealthy(t *testing.T) {
	s, w, r, a := setupReview(t)
	w.xml = pendingReview()
	out, e := r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "review_pending" || w.taps != 0 {
		t.Fatal(out, e, w.taps)
	}
	w.healthy = true
	out, e = r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "healthy" || w.opens != 1 {
		t.Fatal(out, e, w.opens)
	}
	attempt, _ := s.Get(a.ID, "provider-review-attempt")
	if attempt.Phase != "healthy" {
		t.Fatal(attempt)
	}
	w.healthy = false
	w.xml = availableReview()
	out, e = r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "review_pending" || w.taps != 1 {
		t.Fatal(out, e, w.taps)
	}
}
func TestReviewPreviewDisabledIdentityAndCleanup(t *testing.T) {
	for _, which := range []string{"preview", "disabled", "identity", "cleanup"} {
		t.Run(which, func(t *testing.T) {
			s, w, r, a := setupReview(t)
			apply := true
			switch which {
			case "preview":
				apply = false
			case "disabled":
				a.AutoAppeal = false
			case "identity":
				w.binding = false
			case "cleanup":
				w.cleanupFail = true
			}
			out, e := r.Review(context.Background(), a, s, apply)
			switch which {
			case "preview":
				if e != nil || out["would_submit"] != true || w.taps != 0 {
					t.Fatal(out, e, w.taps)
				}
			case "disabled":
				if e != nil || out["state"] != "disabled" || w.opens != 0 {
					t.Fatal(out, e, w.opens)
				}
			case "identity":
				if e == nil || w.taps != 0 {
					t.Fatal(out, e, w.taps)
				}
			case "cleanup":
				if e == nil || out["reason"] != "cleanup_failed" {
					t.Fatal(out, e)
				}
			}
		})
	}
}

func TestReviewApprovalVerificationIsNotUnsubmittedAppeal(t *testing.T) {
	s, w, r, a := setupReview(t)
	w.xml = screenXML(node("This account can now use WhatsApp", ""), node("VERIFY ACCOUNT", actionable))
	out, e := r.Review(context.Background(), a, s, true)
	if e != nil || out["verification_stage"] != "after_approval" || out["review_approved"] != true || out["appeal_confirmed"] != true || w.taps != 0 {
		t.Fatal(out, e, w.taps)
	}
}

func TestReviewUntappedPendingIsNotReportedAsSubmitted(t *testing.T) {
	s, w, r, a := setupReview(t)
	// Someone requested the review by hand between the two screen reads, so
	// the adapter found it pending and did not tap.
	w.untapped = true
	out, e := r.Review(context.Background(), a, s, true)
	if e != nil || out["state"] != "review_pending" || out["submitted_now"] != nil || w.taps != 0 {
		t.Fatal(out, e, w.taps)
	}
}
