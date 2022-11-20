package user

import "go.mau.fi/whatsmeow/types"

type InfoRequest struct {
	Phone string `json:"phone" query:"phone"`
}

type InfoResponseDataDevice struct {
	User   string
	Agent  uint8
	Device string
	Server string
	AD     bool
}

type InfoResponseData struct {
	VerifiedName string                   `json:"verified_name"`
	Status       string                   `json:"status"`
	PictureID    string                   `json:"picture_id"`
	Devices      []InfoResponseDataDevice `json:"devices"`
}

type InfoResponse struct {
	Data []InfoResponseData `json:"data"`
}

type AvatarRequest struct {
	Phone string `json:"phone" query:"phone"`
}

type AvatarResponse struct {
	URL  string `json:"url"`
	ID   string `json:"id"`
	Type string `json:"type"`
}

type MyPrivacySettingResponse struct {
	GroupAdd     string `json:"group_add"`
	LastSeen     string `json:"last_seen"`
	Status       string `json:"status"`
	Profile      string `json:"profile"`
	ReadReceipts string `json:"read_receipts"`
}

type MyListGroupsResponse struct {
	Data []types.GroupInfo `json:"data"`
}
