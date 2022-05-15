package structs

import "go.mau.fi/whatsmeow/types"

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
