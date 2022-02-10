package structs

import (
	"mime/multipart"
)

// ============================== USER ==============================

type UserInfoRequest struct {
	PhoneNumber string `json:"phone_number" query:"phone_number"`
}

type UserInfoResponseDataDevice struct {
	User   string
	Agent  uint8
	Device uint8
	Server string
	AD     bool
}

type UserInfoResponseData struct {
	VerifiedName string                       `json:"verified_name"`
	Status       string                       `json:"status"`
	PictureID    string                       `json:"picture_id"`
	Devices      []UserInfoResponseDataDevice `json:"devices"`
}

type UserInfoResponse struct {
	Data []UserInfoResponseData `json:"data"`
}

type UserAvatarRequest struct {
	PhoneNumber string `json:"phone_number" query:"phone_number"`
}

type UserAvatarResponse struct {
	URL  string `json:"url"`
	ID   string `json:"id"`
	Type string `json:"type"`
}

// ============================== END USER ==============================

// ============================== SEND ==============================

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

// ============================== END SEND ==============================
