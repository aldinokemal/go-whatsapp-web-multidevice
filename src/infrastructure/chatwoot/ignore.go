package chatwoot

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// MessageIdentity names the chat a WhatsApp message belongs to and who sent
// it, each in both address forms WhatsApp uses for the same account: the
// phone-number JID (628…@s.whatsapp.net, or the group JID for ChatJID) and
// the LID (…@lid). Fill in every form that is known and leave the rest empty.
type MessageIdentity struct {
	ChatJID   string
	ChatLID   string
	SenderJID string
	SenderLID string
}

// ShouldIgnoreMessage is the single decision point for the Chatwoot ignore
// lists: every path that mirrors a WhatsApp message to Chatwoot (live
// forwarding, the forward-retry queue, history sync, and any catch-up or
// sent-message mirror added later) must call it and skip the message when it
// returns true.
//
// ignore is the effective list for the device (EffectiveIgnoreJids or
// ResolvedConfig.EffectiveIgnoreJids). The message is ignored when any known
// form of its chat or its sender matches any entry, with the entry syntax of
// CHATWOOT_IGNORE_JIDS: an exact JID, or a suffix wildcard starting with "@"
// ("@g.us", "@s.whatsapp.net", "@lid"). Checking both forms matters because a
// LID that resolves to a phone number is carried as the phone JID in the
// primary field, so an entry written as …@lid would otherwise never match.
func ShouldIgnoreMessage(id MessageIdentity, ignore []string) bool {
	if len(ignore) == 0 {
		return false
	}
	for _, jid := range [...]string{id.ChatJID, id.ChatLID, id.SenderJID, id.SenderLID} {
		if utils.MatchesIgnoredJID(jid, ignore) {
			return true
		}
	}
	return false
}

// EffectiveIgnoreJids returns the ignore list that applies to a device: the
// global CHATWOOT_IGNORE_JIDS plus the device's own ignore_jids. The device
// list adds to the global one and cannot remove an entry from it.
func EffectiveIgnoreJids(deviceIgnoreJids []string) []string {
	if len(deviceIgnoreJids) == 0 {
		return config.ChatwootIgnoreJids
	}
	out := make([]string, 0, len(config.ChatwootIgnoreJids)+len(deviceIgnoreJids))
	out = append(out, config.ChatwootIgnoreJids...)
	return append(out, deviceIgnoreJids...)
}

// EffectiveIgnoreJids is EffectiveIgnoreJids for the device this config was
// resolved for. A nil receiver (legacy callers, no per-device config) yields
// the global list alone.
func (rc *ResolvedConfig) EffectiveIgnoreJids() []string {
	if rc == nil {
		return EffectiveIgnoreJids(nil)
	}
	return EffectiveIgnoreJids(rc.IgnoreJids)
}

// jidFormResolver fills in the other address form of a stored JID from the
// device's LID mapping table. Chat storage keeps one JID per chat and per
// sender: the phone JID when the LID could be resolved at the time, else the
// LID. The cache keeps a history run from repeating a lookup per message.
type jidFormResolver struct {
	lids  lidMappingStore
	cache map[string]string
}

// lidMappingStore is the part of whatsmeow's store.LIDStore used here.
type lidMappingStore interface {
	GetPNForLID(ctx context.Context, lid types.JID) (types.JID, error)
	GetLIDForPN(ctx context.Context, pn types.JID) (types.JID, error)
}

func newJIDFormResolver(waClient *whatsmeow.Client) *jidFormResolver {
	r := &jidFormResolver{cache: make(map[string]string)}
	if waClient != nil && waClient.Store != nil && waClient.Store.LIDs != nil {
		r.lids = waClient.Store.LIDs
	}
	return r
}

// forms returns the phone-JID and LID forms of a stored user JID. The form
// that is not stored is looked up in the mapping table and left empty when
// there is no client, no mapping, or the lookup fails. Group and other
// non-user JIDs are returned as the phone form unchanged.
func (r *jidFormResolver) forms(ctx context.Context, jid string) (phoneJID, lid string) {
	parsed, err := types.ParseJID(jid)
	if err != nil || jid == "" {
		return jid, ""
	}
	parsed = parsed.ToNonAD()
	switch parsed.Server {
	case types.HiddenUserServer:
		return r.other(ctx, parsed), parsed.String()
	case types.DefaultUserServer:
		return parsed.String(), r.other(ctx, parsed)
	default:
		return jid, ""
	}
}

func (r *jidFormResolver) other(ctx context.Context, jid types.JID) string {
	if r.lids == nil {
		return ""
	}
	key := jid.String()
	if cached, ok := r.cache[key]; ok {
		return cached
	}
	var (
		found types.JID
		err   error
	)
	if jid.Server == types.HiddenUserServer {
		found, err = r.lids.GetPNForLID(ctx, jid)
	} else {
		found, err = r.lids.GetLIDForPN(ctx, jid)
	}
	out := ""
	if err == nil && !found.IsEmpty() {
		out = found.ToNonAD().String()
	}
	r.cache[key] = out
	return out
}
