package validations

import (
	"context"
	"testing"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow"
)

func TestValidateJoinGroupWithLink(t *testing.T) {
	type args struct {
		request domainGroup.JoinGroupWithLinkRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid link",
			args: args{request: domainGroup.JoinGroupWithLinkRequest{
				Link: "https://chat.whatsapp.com/ABC123XYZ",
			}},
			err: nil,
		},
		{
			name: "should error with empty link",
			args: args{request: domainGroup.JoinGroupWithLinkRequest{
				Link: "",
			}},
			err: pkgError.ValidationError("link: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJoinGroupWithLink(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateLeaveGroup(t *testing.T) {
	type args struct {
		request domainGroup.LeaveGroupRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id",
			args: args{request: domainGroup.LeaveGroupRequest{
				GroupID: "123456789@g.us",
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.LeaveGroupRequest{
				GroupID: "",
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLeaveGroup(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateCreateGroup(t *testing.T) {
	type args struct {
		request domainGroup.CreateGroupRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid title and participants",
			args: args{request: domainGroup.CreateGroupRequest{
				Title:        "Test Group",
				Participants: []string{"+6281234567890@s.whatsapp.net", "+6281234567891@s.whatsapp.net"},
			}},
			err: nil,
		},
		{
			name: "should error with empty title",
			args: args{request: domainGroup.CreateGroupRequest{
				Title:        "",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
			}},
			err: pkgError.ValidationError("title: cannot be blank."),
		},
		{
			name: "should error with empty participants",
			args: args{request: domainGroup.CreateGroupRequest{
				Title:        "Test Group",
				Participants: []string{},
			}},
			err: pkgError.ValidationError("participants: cannot be blank."),
		},
		{
			name: "should error with nil participants",
			args: args{request: domainGroup.CreateGroupRequest{
				Title:        "Test Group",
				Participants: nil,
			}},
			err: pkgError.ValidationError("participants: cannot be blank."),
		},
		{
			name: "should error with empty participant in array",
			args: args{request: domainGroup.CreateGroupRequest{
				Title:        "Test Group",
				Participants: []string{"+6281234567890@s.whatsapp.net", ""},
			}},
			err: pkgError.ValidationError("participants: (1: cannot be blank.)."),
		},
		{
			name: "should success with single participant",
			args: args{request: domainGroup.CreateGroupRequest{
				Title:        "Test Group",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
			}},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateGroup(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateParticipant(t *testing.T) {
	type args struct {
		request domainGroup.ParticipantRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id and participants",
			args: args{request: domainGroup.ParticipantRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net", "+6281234567891@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantChangeAdd,
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.ParticipantRequest{
				GroupID:      "",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantChangeAdd,
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
		{
			name: "should error with empty participants",
			args: args{request: domainGroup.ParticipantRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{},
				Action:       whatsmeow.ParticipantChangeAdd,
			}},
			err: pkgError.ValidationError("participants: cannot be blank."),
		},
		{
			name: "should error with nil participants",
			args: args{request: domainGroup.ParticipantRequest{
				GroupID:      "123456789@g.us",
				Participants: nil,
				Action:       whatsmeow.ParticipantChangeAdd,
			}},
			err: pkgError.ValidationError("participants: cannot be blank."),
		},
		{
			name: "should error with empty participant in array",
			args: args{request: domainGroup.ParticipantRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net", ""},
				Action:       whatsmeow.ParticipantChangeAdd,
			}},
			err: pkgError.ValidationError("participants: (1: cannot be blank.)."),
		},
		{
			name: "should success with single participant",
			args: args{request: domainGroup.ParticipantRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantChangeRemove,
			}},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParticipant(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateGetGroupRequestParticipants(t *testing.T) {
	type args struct {
		request domainGroup.GetGroupRequestParticipantsRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id",
			args: args{request: domainGroup.GetGroupRequestParticipantsRequest{
				GroupID: "123456789@g.us",
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.GetGroupRequestParticipantsRequest{
				GroupID: "",
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGetGroupRequestParticipants(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateManageGroupRequestParticipants(t *testing.T) {
	type args struct {
		request domainGroup.GroupRequestParticipantsRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid approve action",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantChangeApprove,
			}},
			err: nil,
		},
		{
			name: "should success with valid reject action",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantChangeReject,
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantChangeApprove,
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
		{
			name: "should error with empty participants",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{},
				Action:       whatsmeow.ParticipantChangeApprove,
			}},
			err: pkgError.ValidationError("participants: cannot be blank."),
		},
		{
			name: "should error with nil participants",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: nil,
				Action:       whatsmeow.ParticipantChangeApprove,
			}},
			err: pkgError.ValidationError("participants: cannot be blank."),
		},
		{
			name: "should error with empty participant in array",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net", ""},
				Action:       whatsmeow.ParticipantChangeApprove,
			}},
			err: pkgError.ValidationError("participants: (1: cannot be blank.)."),
		},
		{
			name: "should error with empty action",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
				Action:       "",
			}},
			err: pkgError.ValidationError("action: cannot be blank."),
		},
		{
			name: "should error with invalid action",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantRequestChange("invalid"),
			}},
			err: pkgError.ValidationError("action: must be a valid value."),
		},
		{
			name: "should success with multiple participants",
			args: args{request: domainGroup.GroupRequestParticipantsRequest{
				GroupID:      "123456789@g.us",
				Participants: []string{"+6281234567890@s.whatsapp.net", "+6281234567891@s.whatsapp.net", "+6281234567892@s.whatsapp.net"},
				Action:       whatsmeow.ParticipantChangeApprove,
			}},
			err: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateManageGroupRequestParticipants(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateGetGroupInfoFromLink(t *testing.T) {
	type args struct {
		request domainGroup.GetGroupInfoFromLinkRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid link",
			args: args{request: domainGroup.GetGroupInfoFromLinkRequest{
				Link: "https://chat.whatsapp.com/ABC123XYZ",
			}},
			err: nil,
		},
		{
			name: "should error with empty link",
			args: args{request: domainGroup.GetGroupInfoFromLinkRequest{
				Link: "",
			}},
			err: pkgError.ValidationError("link: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGetGroupInfoFromLink(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSetGroupPhoto(t *testing.T) {
	type args struct {
		request domainGroup.SetGroupPhotoRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id and nil photo",
			args: args{request: domainGroup.SetGroupPhotoRequest{
				GroupID: "123456789@g.us",
				Photo:   nil,
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.SetGroupPhotoRequest{
				GroupID: "",
				Photo:   nil,
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSetGroupPhoto(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestIsImageContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{
			name:        "should return true for image/jpeg",
			contentType: "image/jpeg",
			expected:    true,
		},
		{
			name:        "should return true for image/jpg",
			contentType: "image/jpg",
			expected:    true,
		},
		{
			name:        "should return true for image/png",
			contentType: "image/png",
			expected:    true,
		},
		{
			name:        "should return true for image/gif",
			contentType: "image/gif",
			expected:    true,
		},
		{
			name:        "should return true for image/webp",
			contentType: "image/webp",
			expected:    true,
		},
		{
			name:        "should return false for text/plain",
			contentType: "text/plain",
			expected:    false,
		},
		{
			name:        "should return false for application/pdf",
			contentType: "application/pdf",
			expected:    false,
		},
		{
			name:        "should return false for empty string",
			contentType: "",
			expected:    false,
		},
		{
			name:        "should return false for video/mp4",
			contentType: "video/mp4",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isImageContentType(tt.contentType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateSetGroupName(t *testing.T) {
	type args struct {
		request domainGroup.SetGroupNameRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id and name",
			args: args{request: domainGroup.SetGroupNameRequest{
				GroupID: "123456789@g.us",
				Name:    "Test Group Name",
			}},
			err: nil,
		},
		{
			name: "should success with single character name",
			args: args{request: domainGroup.SetGroupNameRequest{
				GroupID: "123456789@g.us",
				Name:    "A",
			}},
			err: nil,
		},
		{
			name: "should success with 25 character name",
			args: args{request: domainGroup.SetGroupNameRequest{
				GroupID: "123456789@g.us",
				Name:    "1234567890123456789012345",
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.SetGroupNameRequest{
				GroupID: "",
				Name:    "Test Group",
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
		{
			name: "should error with empty name",
			args: args{request: domainGroup.SetGroupNameRequest{
				GroupID: "123456789@g.us",
				Name:    "",
			}},
			err: pkgError.ValidationError("name: cannot be blank."),
		},
		{
			name: "should error with name too long",
			args: args{request: domainGroup.SetGroupNameRequest{
				GroupID: "123456789@g.us",
				Name:    "12345678901234567890123456",
			}},
			err: pkgError.ValidationError("name: the length must be between 1 and 25."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSetGroupName(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSetGroupLocked(t *testing.T) {
	type args struct {
		request domainGroup.SetGroupLockedRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id and locked true",
			args: args{request: domainGroup.SetGroupLockedRequest{
				GroupID: "123456789@g.us",
				Locked:  true,
			}},
			err: nil,
		},
		{
			name: "should success with valid group id and locked false",
			args: args{request: domainGroup.SetGroupLockedRequest{
				GroupID: "123456789@g.us",
				Locked:  false,
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.SetGroupLockedRequest{
				GroupID: "",
				Locked:  true,
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSetGroupLocked(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSetGroupAnnounce(t *testing.T) {
	type args struct {
		request domainGroup.SetGroupAnnounceRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id and announce true",
			args: args{request: domainGroup.SetGroupAnnounceRequest{
				GroupID:  "123456789@g.us",
				Announce: true,
			}},
			err: nil,
		},
		{
			name: "should success with valid group id and announce false",
			args: args{request: domainGroup.SetGroupAnnounceRequest{
				GroupID:  "123456789@g.us",
				Announce: false,
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.SetGroupAnnounceRequest{
				GroupID:  "",
				Announce: true,
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSetGroupAnnounce(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateSetGroupTopic(t *testing.T) {
	type args struct {
		request domainGroup.SetGroupTopicRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id and topic",
			args: args{request: domainGroup.SetGroupTopicRequest{
				GroupID: "123456789@g.us",
				Topic:   "This is a test topic",
			}},
			err: nil,
		},
		{
			name: "should success with valid group id and empty topic",
			args: args{request: domainGroup.SetGroupTopicRequest{
				GroupID: "123456789@g.us",
				Topic:   "",
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.SetGroupTopicRequest{
				GroupID: "",
				Topic:   "Test topic",
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSetGroupTopic(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateGroupInfo(t *testing.T) {
	type args struct {
		request domainGroup.GroupInfoRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid group id",
			args: args{request: domainGroup.GroupInfoRequest{
				GroupID: "123456789@g.us",
			}},
			err: nil,
		},
		{
			name: "should error with empty group id",
			args: args{request: domainGroup.GroupInfoRequest{
				GroupID: "",
			}},
			err: pkgError.ValidationError("group_id: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGroupInfo(context.Background(), tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}
