package whatsapp

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// NormalizeJIDFromLID converts @lid JIDs to their corresponding @s.whatsapp.net JIDs
// Returns the original JID if it's not an @lid or if LID lookup fails
func NormalizeJIDFromLID(ctx context.Context, jid types.JID, client *whatsmeow.Client) types.JID {
	// Only process @lid JIDs
	if jid.Server != "lid" {
		return jid
	}

	// Safety check
	if client == nil || client.Store == nil || client.Store.LIDs == nil {
		log.Warnf("Cannot resolve LID %s: client not available", jid.String())
		return jid
	}

	// Attempt to get the phone number for this LID
	pn, err := client.Store.LIDs.GetPNForLID(ctx, jid)
	if err != nil {
		log.Debugf("Failed to resolve LID %s to phone number: %v", jid.String(), err)
		return jid
	}

	// If we got a valid phone number, use it
	if !pn.IsEmpty() {
		log.Debugf("Resolved LID %s to phone number %s", jid.String(), pn.String())
		return pn
	}

	// Fallback to original JID
	return jid
}

type chatwootQuoteChatKey struct{}

// WithChatwootQuote restricts agent replies to the delivered chat. Numeric
// Chatwoot quotes must also match the chat recorded on their message link.
func WithChatwootQuote(ctx context.Context, linkChat string) context.Context {
	return context.WithValue(ctx, chatwootQuoteChatKey{}, linkChat)
}

// ReplyChatAllowed checks the actual quoted row after recipient validation.
func ReplyChatAllowed(ctx context.Context, recipient, storedChat string) bool {
	linkChat, restricted := ctx.Value(chatwootQuoteChatKey{}).(string)
	if !restricted {
		return true
	}
	return chatwootSameChat(ctx, recipient, storedChat) && (linkChat == "" || chatwootSameChat(ctx, recipient, linkChat))
}

// chatwootSameChat reports whether two JIDs name the same chat. An @lid side
// is mapped to its phone JID by the sending device, as the send path does.
func chatwootSameChat(ctx context.Context, a, b string) bool {
	jidA, errA := utils.ParseJID(a)
	jidB, errB := utils.ParseJID(b)
	if errA != nil || errB != nil {
		return false
	}
	client := ClientFromContext(ctx)
	return utils.ResolveLIDToPhone(ctx, jidA, client).ToNonAD() == utils.ResolveLIDToPhone(ctx, jidB, client).ToNonAD()
}
