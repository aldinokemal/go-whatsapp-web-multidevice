package send

import "encoding/json"

// InteractiveRequest is an experimental, immediate-only native-flow message.
// Params uses WhatsApp's button-specific JSON, not XML in the message body.
type InteractiveRequest struct {
	Phone   string             `json:"phone"`
	Body    string             `json:"body"`
	Footer  string             `json:"footer,omitempty"`
	Buttons []NativeFlowButton `json:"buttons,omitempty"`
	Cards   []InteractiveCard  `json:"cards,omitempty"`
}

type NativeFlowButton struct {
	Name   string          `json:"name"`
	Params json.RawMessage `json:"params"`
}

type InteractiveCard struct {
	ImageBase64 string             `json:"image_base64,omitempty"`
	Title       string             `json:"title,omitempty"`
	Body        string             `json:"body"`
	Footer      string             `json:"footer,omitempty"`
	Buttons     []NativeFlowButton `json:"buttons"`
}
