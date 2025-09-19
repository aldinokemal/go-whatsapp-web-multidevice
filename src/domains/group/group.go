package group

import (
	"mime/multipart"
	"time"

	"go.mau.fi/whatsmeow"
)

// NOTE: IGroupUsecase is now defined in interfaces.go with proper segregation

type JoinGroupWithLinkRequest struct {
	Link string `json:"link" form:"link"`
}

type LeaveGroupRequest struct {
	GroupID string `json:"group_id" form:"group_id"`
}

type CreateGroupRequest struct {
	Title        string   `json:"title" form:"title"`
	Participants []string `json:"participants" form:"participants"`
}

type ParticipantRequest struct {
	GroupID      string                      `json:"group_id" form:"group_id"`
	Participants []string                    `json:"participants" form:"participants"`
	Action       whatsmeow.ParticipantChange `json:"action" form:"action"`
}

type ParticipantStatus struct {
	Participant string `json:"participant"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

type GetGroupParticipantsRequest struct {
	GroupID string `json:"group_id" query:"group_id"`
}

type GroupParticipant struct {
	JID          string `json:"jid"`
	PhoneNumber  string `json:"phone_number"`
	LID          string `json:"lid,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	IsAdmin      bool   `json:"is_admin"`
	IsSuperAdmin bool   `json:"is_super_admin"`
}

type GetGroupParticipantsResponse struct {
	GroupID      string             `json:"group_id"`
	Name         string             `json:"name"`
	Participants []GroupParticipant `json:"participants"`
}

type GetGroupRequestParticipantsRequest struct {
	GroupID string `json:"group_id" query:"group_id"`
}

type GetGroupRequestParticipantsResponse struct {
	JID         string    `json:"jid"`
	RequestedAt time.Time `json:"requested_at"`
}

type GroupRequestParticipantsRequest struct {
	GroupID      string                             `json:"group_id" form:"group_id"`
	Participants []string                           `json:"participants" form:"participants"`
	Action       whatsmeow.ParticipantRequestChange `json:"action" form:"action"`
}

type SetGroupPhotoRequest struct {
	GroupID string                `json:"group_id" form:"group_id"`
	Photo   *multipart.FileHeader `json:"photo" form:"photo"`
}

type SetGroupPhotoResponse struct {
	PictureID string `json:"picture_id"`
	Message   string `json:"message"`
}

type SetGroupNameRequest struct {
	GroupID string `json:"group_id" form:"group_id"`
	Name    string `json:"name" form:"name"`
}

type SetGroupLockedRequest struct {
	GroupID string `json:"group_id" form:"group_id"`
	Locked  bool   `json:"locked" form:"locked"`
}

type SetGroupAnnounceRequest struct {
	GroupID  string `json:"group_id" form:"group_id"`
	Announce bool   `json:"announce" form:"announce"`
}

type SetGroupTopicRequest struct {
	GroupID string `json:"group_id" form:"group_id"`
	Topic   string `json:"topic" form:"topic"`
}

type GetGroupInfoFromLinkRequest struct {
	Link string `json:"link" form:"link"`
}

type GetGroupInfoFromLinkResponse struct {
	GroupID          string    `json:"group_id"`
	Name             string    `json:"name"`
	Topic            string    `json:"topic"`
	CreatedAt        time.Time `json:"created_at"`
	ParticipantCount int       `json:"participant_count"`
	IsLocked         bool      `json:"is_locked"`
	IsAnnounce       bool      `json:"is_announce"`
	IsEphemeral      bool      `json:"is_ephemeral"`
	Description      string    `json:"description"`
}

type GroupInfoRequest struct {
	GroupID string `json:"group_id" query:"group_id"`
}

type GetGroupInviteLinkRequest struct {
	GroupID string `json:"group_id" query:"group_id"`
	Reset   bool   `json:"reset" query:"reset"`
}

type GetGroupInviteLinkResponse struct {
	InviteLink string `json:"invite_link"`
	GroupID    string `json:"group_id"`
}

type GroupInfoResponse struct {
	Data any `json:"data"`
}
