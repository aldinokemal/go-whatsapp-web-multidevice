package send

// ButtonReplyRequest answers a message that carries buttons the way the phone does
// when one of them is tapped: a buttons, template, list or native-flow response that
// quotes the message and carries the selected id, so a bot that checks the id (not
// the text) recognises the choice.
//
// Kind mirrors the "kind" of the selection webhooks already report: buttons, template,
// list or interactive. SelectedID is the button or row id from that message.
type ButtonReplyRequest struct {
	BaseRequest
	ReplyMessageID string  `json:"reply_message_id" form:"reply_message_id"`
	Kind           string  `json:"kind" form:"kind"`
	SelectedID     string  `json:"selected_id" form:"selected_id"`
	Text           string  `json:"text" form:"text"`
	Description    string  `json:"description,omitempty" form:"description"`
	SelectedIndex  *uint32 `json:"selected_index,omitempty" form:"selected_index"`
	// FlowName is the native-flow button name for kind=interactive (default quick_reply).
	FlowName string `json:"flow_name,omitempty" form:"flow_name"`
}
