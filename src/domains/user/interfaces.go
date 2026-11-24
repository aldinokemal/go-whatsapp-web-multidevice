package user

import (
	"context"
)

// IUserInfo handles user information operations
type IUserInfo interface {
	Info(ctx context.Context, request InfoRequest) (response InfoResponse, err error)
	IsOnWhatsApp(ctx context.Context, request CheckRequest) (response CheckResponse, err error)
	BusinessProfile(ctx context.Context, request BusinessProfileRequest) (response BusinessProfileResponse, err error)
	SubscribePresence(ctx context.Context, request SubscribePresenceRequest) (response SubscribePresenceResponse, err error)
}

// IUserProfile handles user profile operations
type IUserProfile interface {
	Avatar(ctx context.Context, request AvatarRequest) (response AvatarResponse, err error)
	ChangeAvatar(ctx context.Context, request ChangeAvatarRequest) (err error)
	ChangePushName(ctx context.Context, request ChangePushNameRequest) (err error)
}

// IUserListing handles user listing operations
type IUserListing interface {
	MyListGroups(ctx context.Context) (response MyListGroupsResponse, err error)
	MyListNewsletter(ctx context.Context) (response MyListNewsletterResponse, err error)
	MyListContacts(ctx context.Context) (response MyListContactsResponse, err error)
	MyListLabels(ctx context.Context) (response MyListLabelsResponse, err error)
	MyListLabelChats(ctx context.Context, labelID string) (response MyListLabelChatsResponse, err error)
	// MySyncLabels rebuilds the stored labels from WhatsApp and returns them.
	MySyncLabels(ctx context.Context) (response MyListLabelsResponse, err error)
}

// IUserContactsDetailed is an optional capability of a user usecase: it is kept
// out of IUserListing so existing implementations of IUserUsecase still compile.
// GET /user/my/contacts/detailed is registered only when the service has it.
type IUserContactsDetailed interface {
	MyListContactsDetailed(ctx context.Context) (response MyListContactsDetailedResponse, err error)
}

// IUserPrivacy handles user privacy operations
type IUserPrivacy interface {
	MyPrivacySetting(ctx context.Context) (response MyPrivacySettingResponse, err error)
}

// IUserUsecase combines all user interfaces for backward compatibility
type IUserUsecase interface {
	IUserInfo
	IUserProfile
	IUserListing
	IUserPrivacy
}
