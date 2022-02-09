package structs

import "time"

type LoginResponse struct {
	ImagePath string        `json:"image_path"`
	Duration  time.Duration `json:"duration"`
	Code      string        `json:"code"`
}

type SendMessageRequest struct {
	PhoneNumber string `json:"phone_number" form:"phone_number"`
	Message     string `json:"message" form:"message"`
}

type SendMessageResponse struct {
	Status string `json:"status"`
}
