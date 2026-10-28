package companion

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

type HTTPConfig struct {
	URL                   string `json:"url"`
	Device                string `json:"device"`
	Phone                 string `json:"phone"`
	LockFile              string `json:"lock_file"`
	BasicAuthEnv          string `json:"basic_auth_env"`
	ConfirmedRemoteLogout bool   `json:"confirmed_remote_logout"`
}
type HTTPBackend struct {
	config HTTPConfig
	client *http.Client
	lock   *fileLock
	paired bool
}

func NewHTTPBackend(c HTTPConfig) (*HTTPBackend, error) {
	u, e := url.Parse(c.URL)
	if e != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || !identifier.MatchString(c.Device) || !phoneNumber.MatchString(c.Phone) || c.LockFile == "" {
		return nil, block("invalid_request")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, block("invalid_request")
	}
	c.URL = strings.TrimRight(c.URL, "/")
	if c.BasicAuthEnv == "" {
		c.BasicAuthEnv = "GOWA_BASIC_AUTH"
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &HTTPBackend{config: c, client: &http.Client{Timeout: 45 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (h *HTTPBackend) api(ctx context.Context, path, method string) (Object, error) {
	req, e := http.NewRequestWithContext(ctx, method, h.config.URL+path, nil)
	if e != nil {
		return nil, block("backend_unavailable")
	}
	req.Header.Set("X-Device-Id", h.config.Device)
	if auth := os.Getenv(h.config.BasicAuthEnv); auth != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth)))
	}
	response, e := h.client.Do(req)
	if e != nil {
		return nil, block("backend_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, block("backend_unavailable")
	}
	var body struct {
		Results Object `json:"results"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body) != nil || (body.Results == nil && method == "GET") {
		return nil, block("backend_unavailable")
	}
	if body.Results == nil {
		body.Results = Object{}
	}
	return body.Results, nil
}
func (h *HTTPBackend) inspect(ctx context.Context) (Object, error) {
	status, e := h.api(ctx, "/devices/"+h.config.Device+"/status", "GET")
	if e != nil {
		return nil, e
	}
	if _, ok := status["is_connected"].(bool); !ok {
		return nil, block("backend_unavailable")
	}
	if _, ok := status["is_logged_in"].(bool); !ok {
		return nil, block("backend_unavailable")
	}
	row, e := h.api(ctx, "/devices/"+h.config.Device, "GET")
	if e != nil {
		return nil, e
	}
	jid := stringValue(row["jid"])
	if jid != "" && !regexp.MustCompile(`^`+regexp.QuoteMeta(h.config.Phone)+`(?::[0-9]+)?@s\.whatsapp\.net$`).MatchString(jid) {
		return nil, block("identity_mismatch")
	}
	saved := 1
	if h.config.ConfirmedRemoteLogout {
		saved = 0
	}
	// The device API normalizes its JID to the account. It cannot establish
	// independent companion identities across servers; advertise that limitation
	// even while logged out, so a multi-host workflow stops before requesting codes.
	return Object{"connected": status["is_connected"] == true, "logged_in": status["is_logged_in"] == true, "companion_jid": jid, "identity_scope": "account", "policy_ok": true, "saved_sessions": saved}, nil
}
func (h *HTTPBackend) Call(ctx context.Context, op string, r Object) (Object, error) {
	if r["device"] != h.config.Device || r["phone"] != h.config.Phone {
		return nil, block("invalid_request")
	}
	switch op {
	case "inspect":
		return h.inspect(ctx)
	case "acquire":
		if h.lock != nil {
			return nil, block("invalid_request")
		}
		l, e := acquireLock(h.config.LockFile)
		if e != nil {
			return nil, e
		}
		h.lock = l
		return Object{"acquired": true}, nil
	}
	if h.lock == nil {
		return nil, block("invalid_request")
	}
	switch op {
	case "reconnect":
		return h.api(ctx, "/devices/"+h.config.Device+"/reconnect", "POST")
	case "pair_code":
		before, e := h.inspect(ctx)
		if e != nil {
			return nil, e
		}
		if before["logged_in"] == true || number(before["saved_sessions"]) > 0 || h.paired {
			return nil, block("credentials_present")
		}
		h.paired = true
		return h.api(ctx, "/devices/"+h.config.Device+"/login/code?phone="+h.config.Phone, "POST")
	}
	return nil, block("invalid_request")
}
func (h *HTTPBackend) Close() error {
	h.client.CloseIdleConnections()
	if h.lock != nil {
		return h.lock.Close()
	}
	return nil
}
