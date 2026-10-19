package whatsapp

import "go.mau.fi/whatsmeow"

// OwnSenderJID returns the JID to record as the sender of a message this device
// sent, in the same non-AD form every other sender in chat storage already uses.
//
// client.Store.ID carries the device suffix (for example
// "6281234567890:32@s.whatsapp.net"), because a linked companion is never
// device 0. Stored verbatim, a sent message becomes the only row in the table
// whose sender is not a plain user JID — and mergeReplyContext copies that value
// straight into ContextInfo.Participant, where a device-suffixed JID matches no
// participant of the chat, so recipients cannot attribute the quoted message.
//
// Every other path already normalises: CreateMessage stores
// normalizedSender.ToNonAD(), history sync uses client.Store.ID.ToNonAD(), and
// MarkAsRead uses client.Store.ID.ToNonAD(). This keeps the two sent-message
// paths consistent with them.
func OwnSenderJID(client *whatsmeow.Client) string {
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return ""
	}
	return client.Store.ID.ToNonAD().String()
}
