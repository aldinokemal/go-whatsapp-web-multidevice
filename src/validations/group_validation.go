package validations

import (
	"context"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"go.mau.fi/whatsmeow"
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

func ValidateGetGroupInfoFromLink(ctx context.Context, request domainGroup.GetGroupInfoFromLinkRequest) error {
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

func ValidateGetGroupParticipants(ctx context.Context, request domainGroup.GetGroupParticipantsRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateGetGroupRequestParticipants(ctx context.Context, request domainGroup.GetGroupRequestParticipantsRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateManageGroupRequestParticipants(ctx context.Context, request domainGroup.GroupRequestParticipantsRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
		validation.Field(&request.Participants, validation.Required),
		validation.Field(&request.Participants, validation.Each(validation.Required)),
		validation.Field(&request.Action, validation.Required, validation.In(whatsmeow.ParticipantChangeApprove, whatsmeow.ParticipantChangeReject)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateSetGroupPhoto(ctx context.Context, request domainGroup.SetGroupPhotoRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
		// Photo can be nil to remove the photo, so it's not required
		// If photo is provided, we could add file type validation here if needed
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	// Optional: Add file type validation if photo is provided
	if request.Photo != nil {
		// Check if it's an image file based on content type or filename
		contentType := request.Photo.Header.Get("Content-Type")
		if contentType != "" && !isImageContentType(contentType) {
			return pkgError.ValidationError("uploaded file must be an image")
		}
	}

	return nil
}

// Helper function to check if content type is an image
func isImageContentType(contentType string) bool {
	imageTypes := []string{
		"image/jpeg",
		"image/jpg",
		"image/png",
		"image/gif",
		"image/webp",
	}

	for _, imageType := range imageTypes {
		if contentType == imageType {
			return true
		}
	}
	return false
}

func ValidateSetGroupName(ctx context.Context, request domainGroup.SetGroupNameRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
		validation.Field(&request.Name, validation.Required, validation.Length(1, 25)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateSetGroupLocked(ctx context.Context, request domainGroup.SetGroupLockedRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
		// Locked is a boolean, no additional validation needed
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateSetGroupAnnounce(ctx context.Context, request domainGroup.SetGroupAnnounceRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
		// Announce is a boolean, no additional validation needed
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateSetGroupTopic(ctx context.Context, request domainGroup.SetGroupTopicRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
		// Topic can be empty to remove the topic, so it's not required
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateGroupInfo(ctx context.Context, request domainGroup.GroupInfoRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateGetGroupInviteLink(ctx context.Context, request domainGroup.GetGroupInviteLinkRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.GroupID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
