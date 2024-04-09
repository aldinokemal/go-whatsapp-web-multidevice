package validations

import (
	"context"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateJoinGroupWithLink(ctx context.Context, request domainGroup.JoinGroupWithLinkRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Link, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateLeaveGroup(ctx context.Context, request domainGroup.LeaveGroupRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateCreateGroup(ctx context.Context, request domainGroup.CreateGroupRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Title, validation.Required),
		validation.Field(&request.Participants, validation.Required),
		validation.Field(&request.Participants, validation.Each(validation.Required)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateParticipant(ctx context.Context, request domainGroup.ParticipantRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
		validation.Field(&request.Participants, validation.Required),
		validation.Field(&request.Participants, validation.Each(validation.Required)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
