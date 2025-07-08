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
