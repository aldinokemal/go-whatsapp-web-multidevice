package user

import (
	"context"
)

type IUserService interface {
	Info(ctx context.Context, request InfoRequest) (response InfoResponse, err error)
	Avatar(ctx context.Context, request AvatarRequest) (response AvatarResponse, err error)
	MyListGroups(ctx context.Context) (response MyListGroupsResponse, err error)
	MyPrivacySetting(ctx context.Context) (response MyPrivacySettingResponse, err error)
}
