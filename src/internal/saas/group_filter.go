package saas

import (
	"strings"

	"github.com/sirupsen/logrus"
)

// IsGroupPayload inspects an outbound webhook payload and returns true
// when it represents a group chat. The SaaS Phase-A agent ignores groups
// entirely (read-only, 1:1 only) so we drop them at the source rather
// than burn bandwidth and a SaaS-side rate-limit slot per message.
//
// Heuristics, in order:
//  1. Explicit `chat_type` field == "group".
//  2. Any JID-shaped field ending in "@g.us" (WhatsApp group suffix).
//
// Safe default: unknown payload shape → NOT a group (forward normally).
// Worst case the SaaS drops it (group filter is duplicated server-side
// in the webhook route as defense in depth).
func IsGroupPayload(payload map[string]any) bool {
	if v, ok := payload["chat_type"].(string); ok && strings.EqualFold(v, "group") {
		return true
	}
	for _, key := range []string{"chat_id", "from", "to", "group_jid"} {
		if v, ok := payload[key].(string); ok && strings.HasSuffix(v, "@g.us") {
			return true
		}
	}
	return false
}

// ShouldDropOutbound returns true when SaaS mode is on AND the payload
// is a group. The caller (the upstream webhook fan-out) skips delivery
// when this returns true.
func ShouldDropOutbound(payload map[string]any) bool {
	if !Enabled() {
		return false
	}
	if !IsGroupPayload(payload) {
		return false
	}
	logrus.Debug("SaaS: dropping group payload before webhook forward")
	return true
}
