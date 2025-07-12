package user

import (
	"mime/multipart"

	"go.mau.fi/whatsmeow/types"
)

type InfoRequest struct {
	Phone string `json:"phone" query:"phone"`
}

type InfoResponseDataDevice struct {
	User   string
	Agent  uint8
	Device string
	Server string
	AD     string
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
	Phone       string `json:"phone" query:"phone"`
	IsPreview   bool   `json:"is_preview" query:"is_preview"`
	IsCommunity bool   `json:"is_community" query:"is_community"`
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

type MyListNewsletterResponse struct {
	Data []types.NewsletterMetadata `json:"data"`
}

type ChangeAvatarRequest struct {
	Avatar *multipart.FileHeader `json:"avatar" form:"avatar"`
}

type MyListContactsResponse struct {
	Data []MyListContactsResponseData `json:"data"`
}

type MyListContactsResponseData struct {
	JID  types.JID `json:"jid"`
	Name string    `json:"name"`
}

type ChangePushNameRequest struct {
	PushName string `json:"push_name" form:"push_name"`
}

type CheckRequest struct {
	Phone string `json:"phone" query:"phone"`
}

type CheckResponse struct {
	IsOnWhatsApp bool `json:"is_on_whatsapp"`
}

type BusinessProfileRequest struct {
	Phone string `json:"phone" query:"phone"`
}

type BusinessProfileCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type BusinessProfileHoursConfig struct {
	DayOfWeek string `json:"day_of_week"`
	Mode      string `json:"mode"`
	OpenTime  string `json:"open_time"`
	CloseTime string `json:"close_time"`
}

type BusinessProfileResponse struct {
	JID                   string                       `json:"jid"`
	Email                 string                       `json:"email"`
	Address               string                       `json:"address"`
	Categories            []BusinessProfileCategory    `json:"categories"`
	ProfileOptions        map[string]string            `json:"profile_options"`
	BusinessHoursTimeZone string                       `json:"business_hours_timezone"`
	BusinessHours         []BusinessProfileHoursConfig `json:"business_hours"`
}
