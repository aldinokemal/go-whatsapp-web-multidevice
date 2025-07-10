package send

type BaseRequest struct {
	Phone       string `json:"phone" form:"phone"`
	Duration    *int   `json:"duration,omitempty" form:"duration"`
	IsForwarded bool   `json:"is_forwarded,omitempty" form:"is_forwarded"`
}
