package group

import (
	"context"
)

// IGroupManagement handles basic group management operations
type IGroupManagement interface {
	JoinGroupWithLink(ctx context.Context, request JoinGroupWithLinkRequest) (groupID string, err error)
	LeaveGroup(ctx context.Context, request LeaveGroupRequest) (err error)
	CreateGroup(ctx context.Context, request CreateGroupRequest) (groupID string, err error)
	GetGroupInfoFromLink(ctx context.Context, request GetGroupInfoFromLinkRequest) (response GetGroupInfoFromLinkResponse, err error)
	GetGroupInviteLink(ctx context.Context, request GetGroupInviteLinkRequest) (response GetGroupInviteLinkResponse, err error)
	GroupInfo(ctx context.Context, request GroupInfoRequest) (response GroupInfoResponse, err error)
}

// IGroupParticipants handles group participant operations
type IGroupParticipants interface {
	ManageParticipant(ctx context.Context, request ParticipantRequest) (result []ParticipantStatus, err error)
	GetGroupParticipants(ctx context.Context, request GetGroupParticipantsRequest) (response GetGroupParticipantsResponse, err error)
	GetGroupRequestParticipants(ctx context.Context, request GetGroupRequestParticipantsRequest) (result []GetGroupRequestParticipantsResponse, err error)
	ManageGroupRequestParticipants(ctx context.Context, request GroupRequestParticipantsRequest) (result []ParticipantStatus, err error)
}

// IGroupSettings handles group settings operations
type IGroupSettings interface {
	SetGroupPhoto(ctx context.Context, request SetGroupPhotoRequest) (pictureID string, err error)
	SetGroupName(ctx context.Context, request SetGroupNameRequest) (err error)
	SetGroupLocked(ctx context.Context, request SetGroupLockedRequest) (err error)
	SetGroupAnnounce(ctx context.Context, request SetGroupAnnounceRequest) (err error)
	SetGroupTopic(ctx context.Context, request SetGroupTopicRequest) (err error)
}

// IGroupUsecase combines all group interfaces for backward compatibility
type IGroupUsecase interface {
	IGroupManagement
	IGroupParticipants
	IGroupSettings
}
