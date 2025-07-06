package send

type PollRequest struct {
	BaseRequest
	Question  string   `json:"question" form:"question"`
	Options   []string `json:"options" form:"options"`
	MaxAnswer int      `json:"max_answer" form:"max_answer"`
}
