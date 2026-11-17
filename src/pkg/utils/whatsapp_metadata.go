package utils

import (
	"encoding/base64"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ExtractConversion extracts conversion attribution (e.g. Click-to-WhatsApp
// entry points) from the message's own ContextInfo. Returns nil when no
// attribution is present.
func ExtractConversion(msg *waE2E.Message) map[string]any {
	ci := ExtractContextInfo(UnwrapMessage(msg))
	if ci == nil {
		return nil
	}

	conversion := make(map[string]any)
	putNonEmptyString(conversion, "conversion_source", ci.GetConversionSource())
	putNonEmptyBytes(conversion, "conversion_data", ci.GetConversionData())
	putNonZeroUint32(conversion, "conversion_delay_seconds", ci.GetConversionDelaySeconds())
	putNonEmptyString(conversion, "entry_point_conversion_source", ci.GetEntryPointConversionSource())
	putNonEmptyString(conversion, "entry_point_conversion_app", ci.GetEntryPointConversionApp())
	putNonZeroUint32(conversion, "entry_point_conversion_delay_seconds", ci.GetEntryPointConversionDelaySeconds())
	putNonEmptyString(conversion, "entry_point_conversion_external_source", ci.GetEntryPointConversionExternalSource())
	putNonEmptyString(conversion, "entry_point_conversion_external_medium", ci.GetEntryPointConversionExternalMedium())

	if len(conversion) == 0 {
		return nil
	}
	return conversion
}

// ExtractMediaMetadata extracts descriptive media metadata (mime type, size,
// dimensions, duration and flags) without any download URL or key material.
// Returns nil when the message carries no media metadata.
func ExtractMediaMetadata(msg *waE2E.Message) map[string]any {
	msg = UnwrapMessage(msg)
	info := make(map[string]any)

	switch {
	case msg.GetImageMessage() != nil:
		image := msg.GetImageMessage()
		putMediaFileFields(info, image.GetMimetype(), image.GetFileLength(), image.GetFileSHA256())
		putNonZeroUint32(info, "width", image.GetWidth())
		putNonZeroUint32(info, "height", image.GetHeight())
	case msg.GetVideoMessage() != nil:
		putVideoFields(info, msg.GetVideoMessage())
	case msg.GetPtvMessage() != nil:
		putVideoFields(info, msg.GetPtvMessage())
	case msg.GetAudioMessage() != nil:
		audio := msg.GetAudioMessage()
		putMediaFileFields(info, audio.GetMimetype(), audio.GetFileLength(), audio.GetFileSHA256())
		putNonZeroUint32(info, "seconds", audio.GetSeconds())
		putTrue(info, "ptt", audio.GetPTT())
	case msg.GetDocumentMessage() != nil:
		document := msg.GetDocumentMessage()
		putMediaFileFields(info, document.GetMimetype(), document.GetFileLength(), document.GetFileSHA256())
		putNonEmptyString(info, "file_name", document.GetFileName())
	case msg.GetStickerMessage() != nil:
		sticker := msg.GetStickerMessage()
		putMediaFileFields(info, sticker.GetMimetype(), sticker.GetFileLength(), sticker.GetFileSHA256())
		putNonZeroUint32(info, "width", sticker.GetWidth())
		putNonZeroUint32(info, "height", sticker.GetHeight())
		putTrue(info, "is_animated", sticker.GetIsAnimated())
	default:
		return nil
	}

	if len(info) == 0 {
		return nil
	}
	return info
}

// ExtractInteractiveReply extracts the user's answer to a buttons, list,
// template button or native flow message. Returns nil when the message is not
// an interactive reply.
func ExtractInteractiveReply(msg *waE2E.Message) map[string]any {
	msg = UnwrapMessage(msg)
	reply := make(map[string]any)

	switch {
	case msg.GetButtonsResponseMessage() != nil:
		buttons := msg.GetButtonsResponseMessage()
		reply["kind"] = "button"
		putNonEmptyString(reply, "id", buttons.GetSelectedButtonID())
		putNonEmptyString(reply, "title", buttons.GetSelectedDisplayText())
	case msg.GetListResponseMessage() != nil:
		list := msg.GetListResponseMessage()
		reply["kind"] = "list"
		putNonEmptyString(reply, "id", list.GetSingleSelectReply().GetSelectedRowID())
		putNonEmptyString(reply, "title", list.GetTitle())
		putNonEmptyString(reply, "description", list.GetDescription())
	case msg.GetTemplateButtonReplyMessage() != nil:
		template := msg.GetTemplateButtonReplyMessage()
		reply["kind"] = "template_button"
		putNonEmptyString(reply, "id", template.GetSelectedID())
		putNonEmptyString(reply, "title", template.GetSelectedDisplayText())
	case msg.GetInteractiveResponseMessage().GetNativeFlowResponseMessage() != nil:
		interactive := msg.GetInteractiveResponseMessage()
		nativeFlow := interactive.GetNativeFlowResponseMessage()
		reply["kind"] = "native_flow"
		putNonEmptyString(reply, "name", nativeFlow.GetName())
		putNonEmptyString(reply, "response_json", nativeFlow.GetParamsJSON())
		putNonEmptyString(reply, "title", interactive.GetBody().GetText())
	default:
		return nil
	}

	return reply
}

// ExtractOrder extracts the identifying fields and totals of an order message.
// Returns nil when the message carries no order data.
func ExtractOrder(msg *waE2E.Message) map[string]any {
	orderMessage := UnwrapMessage(msg).GetOrderMessage()
	if orderMessage == nil {
		return nil
	}

	order := make(map[string]any)
	putNonEmptyString(order, "order_id", orderMessage.GetOrderID())
	putNonEmptyString(order, "title", orderMessage.GetOrderTitle())
	putNonEmptyString(order, "message", orderMessage.GetMessage())
	if itemCount := orderMessage.GetItemCount(); itemCount != 0 {
		order["item_count"] = itemCount
	}
	if totalAmount := orderMessage.GetTotalAmount1000(); totalAmount != 0 {
		order["total_amount_1000"] = totalAmount
	}
	putNonEmptyString(order, "currency", orderMessage.GetTotalCurrencyCode())
	putNonEmptyString(order, "seller_jid", orderMessage.GetSellerJID())
	putNonEmptyString(order, "token", orderMessage.GetToken())

	if len(order) == 0 {
		return nil
	}
	return order
}

func putVideoFields(info map[string]any, video *waE2E.VideoMessage) {
	putMediaFileFields(info, video.GetMimetype(), video.GetFileLength(), video.GetFileSHA256())
	putNonZeroUint32(info, "width", video.GetWidth())
	putNonZeroUint32(info, "height", video.GetHeight())
	putNonZeroUint32(info, "seconds", video.GetSeconds())
	putTrue(info, "gif_playback", video.GetGifPlayback())
}

func putMediaFileFields(info map[string]any, mimeType string, fileLength uint64, fileSHA256 []byte) {
	putNonEmptyString(info, "mime_type", mimeType)
	if fileLength != 0 {
		info["file_length"] = fileLength
	}
	putNonEmptyBytes(info, "file_sha256", fileSHA256)
}

func putNonEmptyString(fields map[string]any, key, value string) {
	if value != "" {
		fields[key] = value
	}
}

func putNonEmptyBytes(fields map[string]any, key string, value []byte) {
	if len(value) > 0 {
		fields[key] = base64.StdEncoding.EncodeToString(value)
	}
}

func putNonZeroUint32(fields map[string]any, key string, value uint32) {
	if value != 0 {
		fields[key] = value
	}
}

func putTrue(fields map[string]any, key string, value bool) {
	if value {
		fields[key] = true
	}
}
