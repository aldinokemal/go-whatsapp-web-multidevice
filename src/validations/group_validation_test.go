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
