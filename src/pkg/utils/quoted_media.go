package utils

import (
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// maxQuotedMediaBytes bounds a stored quote copy, which stays under 64 KiB.
// The inline thumbnail takes most of it, usually a few KiB.
const maxQuotedMediaBytes = 64 << 10

// QuotedMediaCopy returns, marshaled, the copy of a media message that a reply
// quoting it carries: the image, video, video note, audio, document or sticker
// message itself, as the official app quotes it, without its own reply
// context. It returns nil for any other message and when the copy is not under
// 64 KiB.
//
// View-once media keeps only its type: the mimetype, and PTT for audio, with no
// thumbnail, caption or download keys. A view-once document or sticker gets no
// copy. viewOnce reports a view-once wrapper the caller already removed
// (whatsmeow unwraps live messages); one still around msg, as in history sync,
// is found here.
func QuotedMediaCopy(msg *waE2E.Message, viewOnce bool) []byte {
	media, _ := quotedMediaMessage(UnwrapMessage(msg))
	if media == nil {
		return nil
	}
	media = reduceQuotedMedia(media, viewOnce || hasViewOnceWrapper(msg))
	if media == nil {
		return nil
	}
	encoded, err := proto.Marshal(media)
	if err != nil || len(encoded) >= maxQuotedMediaBytes {
		return nil
	}
	return encoded
}

// QuotedMediaFromCopy returns the media message a reply quotes from encoded,
// the stored QuotedMediaCopy of a message of mediaType. It returns nil when
// encoded is empty, not under 64 KiB, unreadable, or anything but a single
// media message of that type. The result is reduced as a new copy would be:
// no reply context, and only the type of view-once media.
func QuotedMediaFromCopy(encoded []byte, mediaType string) *waE2E.Message {
	if len(encoded) == 0 || len(encoded) >= maxQuotedMediaBytes {
		return nil
	}
	decoded := &waE2E.Message{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		return nil
	}
	media, decodedType := quotedMediaMessage(decoded)
	if media == nil || decodedType != mediaType || !proto.Equal(media, decoded) {
		return nil
	}
	return reduceQuotedMedia(media, false)
}

// quotedMediaMessage returns the media message of msg alone in a new envelope,
// picked in the order ExtractMediaInfo uses, and the media type stored for it;
// nil and "" when msg holds none. The media message itself is shared.
func quotedMediaMessage(msg *waE2E.Message) (*waE2E.Message, string) {
	switch {
	case msg.GetImageMessage() != nil:
		return &waE2E.Message{ImageMessage: msg.GetImageMessage()}, "image"
	case msg.GetVideoMessage() != nil:
		return &waE2E.Message{VideoMessage: msg.GetVideoMessage()}, "video"
	case msg.GetPtvMessage() != nil:
		return &waE2E.Message{PtvMessage: msg.GetPtvMessage()}, "video_note"
	case msg.GetAudioMessage() != nil:
		return &waE2E.Message{AudioMessage: msg.GetAudioMessage()}, "audio"
	case msg.GetDocumentMessage() != nil:
		return &waE2E.Message{DocumentMessage: msg.GetDocumentMessage()}, "document"
	case msg.GetStickerMessage() != nil:
		return &waE2E.Message{StickerMessage: msg.GetStickerMessage()}, "sticker"
	}
	return nil, ""
}

// reduceQuotedMedia returns a copy of media, an envelope from
// quotedMediaMessage, without the reply context of its media message. View-once
// media (viewOnce, or flagged on the media message) keeps only its type; nil
// for a view-once document or sticker.
func reduceQuotedMedia(media *waE2E.Message, viewOnce bool) *waE2E.Message {
	media = proto.Clone(media).(*waE2E.Message)
	switch {
	case media.ImageMessage != nil:
		image := media.ImageMessage
		image.ContextInfo = nil
		if viewOnce || image.GetViewOnce() {
			media.ImageMessage = &waE2E.ImageMessage{Mimetype: image.Mimetype, ViewOnce: proto.Bool(true)}
		}
	case media.VideoMessage != nil:
		media.VideoMessage = reduceQuotedVideo(media.VideoMessage, viewOnce)
	case media.PtvMessage != nil:
		media.PtvMessage = reduceQuotedVideo(media.PtvMessage, viewOnce)
	case media.AudioMessage != nil:
		audio := media.AudioMessage
		audio.ContextInfo = nil
		if viewOnce || audio.GetViewOnce() {
			media.AudioMessage = &waE2E.AudioMessage{Mimetype: audio.Mimetype, PTT: audio.PTT, ViewOnce: proto.Bool(true)}
		}
	case media.DocumentMessage != nil:
		if viewOnce {
			return nil
		}
		media.DocumentMessage.ContextInfo = nil
	case media.StickerMessage != nil:
		if viewOnce {
			return nil
		}
		media.StickerMessage.ContextInfo = nil
	}
	return media
}

func reduceQuotedVideo(video *waE2E.VideoMessage, viewOnce bool) *waE2E.VideoMessage {
	video.ContextInfo = nil
	if viewOnce || video.GetViewOnce() {
		return &waE2E.VideoMessage{Mimetype: video.Mimetype, ViewOnce: proto.Bool(true)}
	}
	return video
}

// hasViewOnceWrapper reports whether UnwrapMessage passes a view-once wrapper
// on its way to the inner message of msg.
func hasViewOnceWrapper(msg *waE2E.Message) bool {
	for i := 0; i < 3 && msg != nil; i++ { // the bound UnwrapMessage uses
		switch {
		case msg.GetViewOnceMessage().GetMessage() != nil,
			msg.GetViewOnceMessageV2().GetMessage() != nil,
			msg.GetViewOnceMessageV2Extension().GetMessage() != nil:
			return true
		}
		msg = msg.GetEphemeralMessage().GetMessage()
	}
	return false
}
