package group

import (
	"context"
	"mime/multipart"
	"time"

	"go.mau.fi/whatsmeow"
)

type IGroupUsecase interface {
	JoinGroupWithLink(ctx context.Context, request JoinGroupWithLinkRequest) (groupID string, err error)
	LeaveGroup(ctx context.Context, request LeaveGroupRequest) (err error)
	CreateGroup(ctx context.Context, request CreateGroupRequest) (groupID string, err error)
	ManageParticipant(ctx context.Context, request ParticipantRequest) (result []ParticipantStatus, err error)
	GetGroupRequestParticipants(ctx context.Context, request GetGroupRequestParticipantsRequest) (result []GetGroupRequestParticipantsResponse, err error)
	ManageGroupRequestParticipants(ctx context.Context, request GroupRequestParticipantsRequest) (result []ParticipantStatus, err error)
	SetGroupPhoto(ctx context.Context, request SetGroupPhotoRequest) (pictureID string, err error)
	SetGroupName(ctx context.Context, request SetGroupNameRequest) (err error)
	SetGroupLocked(ctx context.Context, request SetGroupLockedRequest) (err error)
	SetGroupAnnounce(ctx context.Context, request SetGroupAnnounceRequest) (err error)
	SetGroupTopic(ctx context.Context, request SetGroupTopicRequest) (err error)
}

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
