package structs

import (
	"go.mau.fi/whatsmeow/types"
	"mime/multipart"
)

// ============================== USER ==============================

type UserInfoRequest struct {
	Phone string `json:"phone" query:"phone"`
}

type UserInfoResponseDataDevice struct {
	User   string
	Agent  uint8
	Device string
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
	Phone string `json:"phone" query:"phone"`
}

type UserAvatarResponse struct {
	URL  string `json:"url"`
	ID   string `json:"id"`
	Type string `json:"type"`
}

type UserMyPrivacySettingResponse struct {
	GroupAdd     string `json:"group_add"`
	LastSeen     string `json:"last_seen"`
	Status       string `json:"status"`
	Profile      string `json:"profile"`
	ReadReceipts string `json:"read_receipts"`
}

type UserMyListGroupsResponse struct {
	Data []types.GroupInfo `json:"data"`
}

// ============================== END USER ==============================

// ============================== SEND ==============================

type SendMessageRequest struct {
	Phone   string `json:"phone" form:"phone"`
	Message string `json:"message" form:"message"`
}

type SendMessageResponse struct {
	Status string `json:"status"`
}

type SendImageRequest struct {
	Phone    string                `json:"phone" form:"phone"`
	Caption  string                `json:"caption" form:"caption"`
	Image    *multipart.FileHeader `json:"image" form:"image"`
	ViewOnce bool                  `json:"view_once" form:"view_once"`
}

type SendImageResponse struct {
	Status string `json:"status"`
}

type SendFileRequest struct {
	Phone string                `json:"phone" form:"phone"`
	File  *multipart.FileHeader `json:"file" form:"file"`
}

type SendFileResponse struct {
	Status string `json:"status"`
}

// ============================== END SEND ==============================
