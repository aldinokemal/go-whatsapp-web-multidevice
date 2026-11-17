package utils

import (
	"encoding/json"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// ExtractContextMetadata returns persisted reply context as a JSON string.
// Live, sent, and history ingestion share this extraction, including disappearing
// and view-once wrappers. An empty string means no reply context was supplied;
// storage preserves any previously known context when replaying such a message.
func ExtractContextMetadata(msg *waE2E.Message) string {
	contextInfo := ExtractContextInfo(UnwrapMessage(msg))
	if contextInfo.GetStanzaID() == "" {
		return ""
	}
	metadata := struct {
		RepliedToID string `json:"replied_to_id"`
	}{RepliedToID: contextInfo.GetStanzaID()}
	// A struct containing only a string is always JSON-marshalable.
	encoded, _ := json.Marshal(metadata)
	return string(encoded)
}
