package send

type LinkRequest struct {
	Phone       string `json:"phone" form:"phone"`
	Caption     string `json:"caption"`
	Link        string `json:"link"`
	IsForwarded bool   `json:"is_forwarded" form:"is_forwarded"`
}
