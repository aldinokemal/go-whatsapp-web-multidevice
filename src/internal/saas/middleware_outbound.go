package saas

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"time"
)

// SignOutboundRequest attaches the three SaaS headers (`X-Saas-Org-Id`,
// `X-Bot-Timestamp`, `X-Bot-Token-Hmac`) on an outbound webhook request.
// Caller passes the raw body bytes — the HMAC is over them exactly, so any
// re-serialisation after this point breaks the signature.
//
// No-op when SaaS integration is disabled (env vars unset). The caller
// can keep its existing headers regardless.
//
// Algorithm: HMAC-SHA256(secret, body), hex-encoded. The timestamp header
// protects against replay; the SaaS rejects deliveries with drift > ±5
// minutes. Both header values are covered by the signature implicitly
// (the SaaS recomputes against the same body bytes it received).
func SignOutboundRequest(req *http.Request, body []byte) {
	c := Load()
	if c.WebhookURL == "" || c.WebhookSecret == "" {
		return
	}
	if c.OrgID != "" {
		req.Header.Set("X-Saas-Org-Id", c.OrgID)
	}

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Bot-Timestamp", ts)

	mac := hmac.New(sha256.New, []byte(c.WebhookSecret))
	mac.Write(body)
	req.Header.Set("X-Bot-Token-Hmac", hex.EncodeToString(mac.Sum(nil)))
}

// OverrideWebhookURL returns the SaaS webhook URL when SaaS mode is on,
// else returns the upstream-configured URL unchanged. Lets the upstream
// `submitWebhook` keep its loop over configured URLs while we redirect
// to the SaaS endpoint without touching the upstream config list.
func OverrideWebhookURL(upstream string) string {
	c := Load()
	if c.WebhookURL == "" {
		return upstream
	}
	return c.WebhookURL
}
