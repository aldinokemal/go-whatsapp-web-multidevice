package companion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func httpConfig(t *testing.T, url string) HTTPConfig {
	return HTTPConfig{URL: url, Device: "sales", Phone: testPhone, LockFile: filepath.Join(t.TempDir(), "host.lock")}
}
func TestHTTPRejectsRemotePlaintextAndCredentialURLs(t *testing.T) {
	for _, url := range []string{"http://remote.example", "https://user:secret@example.test", "https://example.test/?token=secret", "https://example.test/#secret", "http:///bad", "ftp://localhost", "https://example.test/?"} {
		if _, e := NewHTTPBackend(httpConfig(t, url)); e == nil {
			t.Fatal(url)
		}
	}
}
func TestHTTPRequiresConfirmedLogoutAndLock(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Device-Id") != "sales" {
			t.Error("missing device scope")
		}
		if r.Method == "POST" {
			posts++
		}
		result := Object{}
		if strings.HasSuffix(r.URL.Path, "/status") {
			result = Object{"is_connected": false, "is_logged_in": false}
		}
		if strings.HasSuffix(r.URL.Path, "/login/code") {
			result = Object{"pair_code": "ABCD-1234"}
		}
		json.NewEncoder(w).Encode(Object{"results": result})
	}))
	defer server.Close()
	for _, confirmed := range []bool{false, true} {
		c := httpConfig(t, server.URL)
		c.ConfirmedRemoteLogout = confirmed
		h, e := NewHTTPBackend(c)
		if e != nil {
			t.Fatal(e)
		}
		defer h.Close()
		req := Object{"device": "sales", "phone": testPhone}
		s, e := h.Call(context.Background(), "inspect", req)
		if e != nil || number(s["saved_sessions"]) != map[bool]float64{false: 1, true: 0}[confirmed] {
			t.Fatal(s, e)
		}
		if _, e = h.Call(context.Background(), "pair_code", req); e == nil {
			t.Fatal("paired without lock")
		}
		if _, e = h.Call(context.Background(), "acquire", req); e != nil {
			t.Fatal(e)
		}
		_, e = h.Call(context.Background(), "pair_code", req)
		if confirmed && e != nil || !confirmed && e == nil {
			t.Fatal(e)
		}
		if _, e = h.Call(context.Background(), "pair_code", req); e == nil {
			t.Fatal("duplicate code request")
		}
	}
	if posts != 1 {
		t.Fatal(posts)
	}
}
func TestHTTPRefusesRedirectAndWrongIdentity(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(map[bool]string{false: "identity", true: "redirect"}[redirect], func(t *testing.T) {
			received := 0
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { received++ }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if redirect {
					http.Redirect(w, r, target.URL, http.StatusFound)
					return
				}
				json.NewEncoder(w).Encode(Object{"results": Object{"jid": "15559999999:1@s.whatsapp.net"}})
			}))
			defer server.Close()
			h, e := NewHTTPBackend(httpConfig(t, server.URL))
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			_, e = h.Call(context.Background(), "inspect", Object{"device": "sales", "phone": testPhone})
			if e == nil || received != 0 {
				t.Fatal(e, received)
			}
		})
	}
}
func TestHTTPAdapterBoundToExplicitDevice(t *testing.T) {
	h, e := NewHTTPBackend(httpConfig(t, "http://localhost:9999"))
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	if _, e = h.Call(context.Background(), "acquire", Object{"device": "other", "phone": testPhone}); e == nil {
		t.Fatal("switched device")
	}
}
func TestADBRequiresExplicitProfileAndPrivateEvidence(t *testing.T) {
	for _, config := range []Object{{"serial": "test", "state_dir": t.TempDir()}, {"serial": "test", "user": -1, "state_dir": t.TempDir()}, {"serial": "../test", "user": 0, "state_dir": t.TempDir()}, {"serial": "test", "user": 0, "state_dir": t.TempDir(), "package": "other.app"}} {
		if _, e := NewADB(config, Account{}); e == nil {
			t.Fatal(config)
		}
	}
}
func TestHTTPReconnectAcceptsUpstreamNullResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/reconnect") {
			t.Error(r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":200,"results":null}`))
	}))
	defer server.Close()
	h, e := NewHTTPBackend(httpConfig(t, server.URL))
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	r := Object{"device": "sales", "phone": testPhone}
	if _, e = h.Call(context.Background(), "acquire", r); e != nil {
		t.Fatal(e)
	}
	if _, e = h.Call(context.Background(), "reconnect", r); e != nil {
		t.Fatal(e)
	}
}

func TestHTTPReportsNormalizedAccountIdentityScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := Object{"jid": testPhone + "@s.whatsapp.net"}
		if strings.HasSuffix(r.URL.Path, "/status") {
			result = Object{"is_connected": true, "is_logged_in": true}
		}
		json.NewEncoder(w).Encode(Object{"results": result})
	}))
	defer server.Close()
	h, err := NewHTTPBackend(httpConfig(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	result, err := h.Call(context.Background(), "inspect", Object{"device": "sales", "phone": testPhone})
	if err != nil || result["identity_scope"] != "account" || result["companion_jid"] != testPhone+"@s.whatsapp.net" {
		t.Fatal(result, err)
	}
}
