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
	Name         string                   `json:"name"`
	VerifiedName string                   `json:"verified_name"`
	Status       string                   `json:"status"`
	PictureID    string                   `json:"picture_id"`
	Devices      []InfoResponseDataDevice `json:"devices"`
}

type InfoResponse struct {
	Data          []InfoResponseData `json:"data"`
	ResolvedPhone string             `json:"resolved_phone,omitempty"`
	ResolvedLID   string             `json:"resolved_lid,omitempty"`
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

// MyListLabelsResponse lists the device's WhatsApp labels: WhatsApp Business
// labels and the chat lists of the consumer app.
type MyListLabelsResponse struct {
	Data []MyListLabelsResponseData `json:"data"`
}

type MyListLabelsResponseData struct {
	LabelID      string `json:"label_id"`
	Name         string `json:"name"`
	Color        int32  `json:"color"`
	Type         string `json:"type,omitempty"`
	PredefinedID int32  `json:"predefined_id,omitempty"`
	OrderIndex   int32  `json:"order_index"`
	IsActive     bool   `json:"is_active"`
	IsImmutable  bool   `json:"is_immutable"`
	ChatCount    int    `json:"chat_count"`
}

type MyListLabelChatsResponse struct {
	LabelID string                         `json:"label_id"`
	Data    []MyListLabelChatsResponseData `json:"data"`
}

// MyListLabelChatsResponseData is one labeled chat. JID is the phone JID when
// the chat's LID resolves to one; LID is set when the label was stored for a LID.
type MyListLabelChatsResponseData struct {
	JID string `json:"jid"`
	LID string `json:"lid,omitempty"`
}

// MyListContactsDetailedResponse lists the device's person contacts with each
// contact-store name in its own field, so a client can tell the name saved in
// the address book from the contact's own push name or business name.
type MyListContactsDetailedResponse struct {
	Data []MyListContactsDetailedData `json:"data"`
}

type MyListContactsDetailedData struct {
	JID          string `json:"jid"`
	LID          string `json:"lid,omitempty"`
	FullName     string `json:"full_name,omitempty"`
	FirstName    string `json:"first_name,omitempty"`
	PushName     string `json:"push_name,omitempty"`
	BusinessName string `json:"business_name,omitempty"`
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

type SubscribePresenceRequest struct {
	Phone string `json:"phone" query:"phone"`
}

type SubscribePresenceResponse struct {
	Subscribed bool   `json:"subscribed"`
	JID        string `json:"jid"`
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
