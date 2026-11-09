package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
)

// addMessageMetadataFields adds conversion attribution, media metadata,
// interactive replies, order fields and the message's own ContextInfo counters
// to a regular message payload. Absent values are omitted rather than sent
// empty.
func addMessageMetadataFields(ctx context.Context, client *whatsmeow.Client, msg *waE2E.Message, payload map[string]any) {
	if conversion := utils.ExtractConversion(msg); conversion != nil {
		payload["conversion"] = conversion
	}

	if mediaInfo := utils.ExtractMediaMetadata(msg); mediaInfo != nil {
		payload["media_info"] = mediaInfo
	}

	if reply := utils.ExtractInteractiveReply(msg); reply != nil {
		payload["interactive_reply"] = reply
		if _, hasBody := payload["body"]; !hasBody {
			if title, _ := reply["title"].(string); title != "" {
				payload["body"] = title
			}
		}
	}

	if order := utils.ExtractOrder(msg); order != nil {
		if raw, ok := payload["order"].(*waE2E.OrderMessage); ok && raw != nil {
			payload["order"] = webhookOrderPayload{OrderMessage: raw, fields: order}
		}
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

// webhookOrderPayload keeps the raw OrderMessage embedded so existing readers
// of payload["order"] (e.g. GetOrderTitle in the Chatwoot forward) still work,
// while its JSON is the proto's own encoding plus the snake_case order fields.
type webhookOrderPayload struct {
	*waE2E.OrderMessage
	fields map[string]any
}

func (p webhookOrderPayload) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(p.OrderMessage)
	if err != nil {
		return nil, err
	}

	var merged map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&merged); err != nil {
		return nil, err
	}
	if merged == nil {
		merged = make(map[string]any, len(p.fields))
	}
	maps.Copy(merged, p.fields)
	return json.Marshal(merged)
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
