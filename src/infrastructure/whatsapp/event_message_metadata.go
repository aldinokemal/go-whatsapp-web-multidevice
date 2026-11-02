package whatsapp

import (
	"context"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
)

// addMessageMetadataFields adds conversion attribution, media metadata and the
// message's own ContextInfo counters to a regular message payload. Absent
// values are omitted rather than sent empty.
func addMessageMetadataFields(ctx context.Context, client *whatsmeow.Client, msg *waE2E.Message, payload map[string]any) {
	if conversion := utils.ExtractConversion(msg); conversion != nil {
		payload["conversion"] = conversion
	}

	if mediaInfo := utils.ExtractMediaMetadata(msg); mediaInfo != nil {
		payload["media_info"] = mediaInfo
	}

	ci := utils.ExtractContextInfo(utils.UnwrapMessage(msg))
	if ci == nil {
		return
	}

	if score := ci.GetForwardingScore(); score != 0 {
		payload["forwarding_score"] = score
	}

	if mentions := normalizeMentionedJIDs(ctx, client, ci.GetMentionedJID()); len(mentions) > 0 {
		payload["mentioned_jids"] = mentions
	}

	if expiration := ci.GetExpiration(); expiration != 0 {
		payload["expiration"] = expiration
	}
}

func normalizeMentionedJIDs(ctx context.Context, client *whatsmeow.Client, mentions []string) []string {
	canResolve := client != nil && client.Store != nil && client.Store.LIDs != nil
	var normalized []string
	for _, mention := range mentions {
		if mention == "" {
			continue
		}
		if canResolve {
			if jid, err := types.ParseJID(mention); err == nil && jid.Server == types.HiddenUserServer {
				if resolved := NormalizeJIDFromLID(ctx, jid, client); resolved != jid {
					mention = resolved.ToNonAD().String()
				}
			}
		}
		normalized = append(normalized, mention)
	}
	return normalized
}
