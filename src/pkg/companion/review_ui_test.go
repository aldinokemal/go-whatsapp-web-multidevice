package companion

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
)

type rawUITransport struct{ uiTransport }

func (t *rawUITransport) CaptureRaw(ctx context.Context) (string, error) { return t.Capture(ctx) }

func reviewAccountObject() Object {
	a := testAccount()
	return Object{"id": a.ID, "phone": a.Phone, "display_name": a.DisplayName}
}
func screenHash(xml string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(xml))) }

func TestReviewWorkerTapsOnceOnApprovedScreen(t *testing.T) {
	ctx := context.Background()
	ban := availableReview()
	transport := &rawUITransport{uiTransport{screens: []string{ban, ban, pendingReview()}}}
	w := &PhoneWorker{NewTransport: func(Object, Account) (UITransport, error) { return transport, nil }}
	out, e := w.Call(ctx, "review_begin", Object{"account": reviewAccountObject(), "config": Object{"package": testPackage}})
	if e != nil || out["identity_verified"] != true || out["identity_source"] != "device_binding" || out["xml"] != ban {
		t.Fatal(out, e)
	}
	w.Review.Settle = 1
	out, e = w.Call(ctx, "review_request", Object{"screen_sha256": screenHash(ban)})
	if e != nil || out["tapped"] != true || out["xml"] != pendingReview() {
		t.Fatal(out, e)
	}
	if fmt.Sprint(transport.actions) != "[start tap:60:35]" {
		t.Fatal(transport.actions)
	}
	if _, e = w.Call(ctx, "review_request", Object{"screen_sha256": screenHash(ban)}); e == nil {
		t.Fatal("second request accepted")
	}
	if _, e = w.Call(ctx, "finish", Object{}); e != nil || !transport.released || transport.restored != "" {
		t.Fatal(e, transport.released, transport.restored)
	}
}

func TestReviewRequestNeverTapsAnotherScreen(t *testing.T) {
	ctx := context.Background()
	ban := availableReview()
	for _, tc := range []struct {
		name, hash, now string
		wantErr         bool
	}{
		{"hash of another screen", screenHash(pendingReview()), ban, true},
		{"screen changed to pending", screenHash(ban), pendingReview(), false},
		{"button disabled", screenHash(ban), screenXML(node("This account can't use WhatsApp", ""), node("REQUEST REVIEW", `clickable="true" enabled="false" bounds="[10,10][110,60]"`)), false},
		{"another number on screen", screenHash(ban), screenXML(node("This account can't use WhatsApp", ""), node("+1 555 999 9999", ""), node("REQUEST REVIEW", actionable)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &rawUITransport{uiTransport{screens: []string{ban, tc.now}}}
			u := &ReviewUI{Transport: transport, Account: testAccount(), Package: testPackage, Settle: 1}
			if _, e := u.Begin(ctx); e != nil {
				t.Fatal(e)
			}
			out, e := u.Request(ctx, tc.hash)
			if (e != nil) != tc.wantErr {
				t.Fatal(out, e)
			}
			if e == nil && out["tapped"] != false {
				t.Fatal(out)
			}
			if fmt.Sprint(transport.actions) != "[start]" {
				t.Fatal("tapped", transport.actions)
			}
		})
	}
}

func TestReviewBeginBindsVisibleNumber(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		number, source string
		ok             bool
	}{{"+1 555 123 4567", "screen_number", true}, {"+1 555 999 9999", "", false}} {
		t.Run(tc.number, func(t *testing.T) {
			screen := screenXML(node("This account can't use WhatsApp", ""), node(tc.number, ""), node("REQUEST REVIEW", actionable))
			u := &ReviewUI{Transport: &rawUITransport{uiTransport{screens: []string{screen}}}, Account: testAccount(), Package: testPackage}
			out, e := u.Begin(ctx)
			if tc.ok != (e == nil) || (tc.ok && out["identity_source"] != tc.source) {
				t.Fatal(out, e)
			}
		})
	}
}

func TestReviewNeedsRawCapture(t *testing.T) {
	u := &ReviewUI{Transport: &uiTransport{screens: []string{availableReview()}}, Account: testAccount(), Package: testPackage}
	if _, e := u.Begin(context.Background()); e == nil {
		t.Fatal("review ran on a transport without raw capture")
	}
}

func TestWorkerServesOnlyOneSession(t *testing.T) {
	ctx := context.Background()
	transport := &rawUITransport{uiTransport{screens: []string{availableReview()}}}
	w := &PhoneWorker{NewTransport: func(Object, Account) (UITransport, error) { return transport, nil }}
	if _, e := w.Call(ctx, "review_begin", Object{"account": reviewAccountObject()}); e != nil {
		t.Fatal(e)
	}
	if _, e := w.Call(ctx, "begin", Object{"account": reviewAccountObject()}); e == nil {
		t.Fatal("pairing began on a review session")
	}
	if _, e := w.Call(ctx, "prepare_pair", Object{}); e == nil {
		t.Fatal("pairing op accepted on a review session")
	}
}
