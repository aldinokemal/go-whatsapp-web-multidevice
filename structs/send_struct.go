package structs

import "mime/multipart"

type SendMessageRequest struct {
	PhoneNumber string `json:"phone_number" form:"phone_number"`
	Message     string `json:"message" form:"message"`
}

type SendMessageResponse struct {
	Status string `json:"status"`
}

type SendImageRequest struct {
	PhoneNumber string                `json:"phone_number" form:"phone_number"`
	Caption     string                `json:"caption" form:"caption"`
	Image       *multipart.FileHeader `json:"image" form:"image"`
	ViewOnce    bool                  `json:"view_once" form:"view_once"`
}

type SendImageResponse struct {
	Status string `json:"status"`
}
