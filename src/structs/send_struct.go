package structs

import (
	"mime/multipart"
)

type SendType string

const TypeUser SendType = "user"
const TypeGroup SendType = "group"

type SendMessageRequest struct {
	Phone   string   `json:"phone" form:"phone"`
	Message string   `json:"message" form:"message"`
	Type    SendType `json:"type" form:"type"`
}

type SendMessageResponse struct {
	Status string `json:"status"`
}

type SendImageRequest struct {
	Phone    string                `json:"phone" form:"phone"`
	Caption  string                `json:"caption" form:"caption"`
	Image    *multipart.FileHeader `json:"image" form:"image"`
	ViewOnce bool                  `json:"view_once" form:"view_once"`
	Type     SendType              `json:"type" form:"message"`
}

type SendImageResponse struct {
	Status string `json:"status"`
}

type SendFileRequest struct {
	Phone string                `json:"phone" form:"phone"`
	File  *multipart.FileHeader `json:"file" form:"file"`
	Type  SendType              `json:"type" form:"message"`
}

type SendFileResponse struct {
	Status string `json:"status"`
}
