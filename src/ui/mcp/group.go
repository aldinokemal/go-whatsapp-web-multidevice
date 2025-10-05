package mcp

import (
	"context"
	"fmt"
	"strings"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mau.fi/whatsmeow"
)

type GroupHandler struct {
	groupService domainGroup.IGroupUsecase
}

func InitMcpGroup(groupService domainGroup.IGroupUsecase) *GroupHandler {
	return &GroupHandler{groupService: groupService}
}

func (h *GroupHandler) AddGroupTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolCreateGroup(), h.handleCreateGroup)
	mcpServer.AddTool(h.toolJoinGroup(), h.handleJoinGroup)
	mcpServer.AddTool(h.toolLeaveGroup(), h.handleLeaveGroup)
	mcpServer.AddTool(h.toolGetParticipants(), h.handleGetParticipants)
	mcpServer.AddTool(h.toolManageParticipants(), h.handleManageParticipants)
	mcpServer.AddTool(h.toolGetInviteLink(), h.handleGetInviteLink)
	mcpServer.AddTool(h.toolGroupInfo(), h.handleGroupInfo)
	mcpServer.AddTool(h.toolSetGroupName(), h.handleSetGroupName)
	mcpServer.AddTool(h.toolSetGroupTopic(), h.handleSetGroupTopic)
	mcpServer.AddTool(h.toolSetGroupLocked(), h.handleSetGroupLocked)
	mcpServer.AddTool(h.toolSetGroupAnnounce(), h.handleSetGroupAnnounce)
	mcpServer.AddTool(h.toolListGroupJoinRequests(), h.handleListGroupJoinRequests)
	mcpServer.AddTool(h.toolManageGroupJoinRequests(), h.handleManageGroupJoinRequests)
}

func (h *GroupHandler) toolCreateGroup() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_create",
		mcp.WithDescription("Create a new WhatsApp group with an optional participant list."),
		mcp.WithTitleAnnotation("Create Group"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("title",
			mcp.Description("Group subject/title."),
			mcp.Required(),
		),
		mcp.WithArray("participants",
			mcp.Description("Phone numbers to add during creation (without @s.whatsapp.net suffix)."),
			mcp.WithStringItems(),
		),
	)
}

func (h *GroupHandler) handleCreateGroup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	title, err := request.RequireString("title")
	if err != nil {
		return nil, err
	}

	var participants []string
	if args := request.GetArguments(); args != nil {
		if raw, ok := args["participants"]; ok {
			participants, err = toStringSlice(raw)
			if err != nil {
				return nil, err
			}
		}
	}

	groupID, err := h.groupService.CreateGroup(ctx, domainGroup.CreateGroupRequest{
		Title:        strings.TrimSpace(title),
		Participants: participants,
	})
	if err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group_id": groupID,
		"title":    strings.TrimSpace(title),
		"members":  len(participants),
	}

	fallback := fmt.Sprintf("Created group %s with %d members", groupID, len(participants))
	return mcp.NewToolResultStructured(structured, fallback), nil
}

func (h *GroupHandler) toolJoinGroup() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_join_via_link",
		mcp.WithDescription("Join a group using an invite link."),
		mcp.WithTitleAnnotation("Join Group"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("invite_link",
			mcp.Description("WhatsApp group invite link."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleJoinGroup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	link, err := request.RequireString("invite_link")
	if err != nil {
		return nil, err
	}

	groupID, err := h.groupService.JoinGroupWithLink(ctx, domainGroup.JoinGroupWithLinkRequest{Link: strings.TrimSpace(link)})
	if err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group_id":    groupID,
		"invite_link": link,
	}

	fallback := fmt.Sprintf("Joined group %s", groupID)
	return mcp.NewToolResultStructured(structured, fallback), nil
}

func (h *GroupHandler) toolLeaveGroup() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_leave",
		mcp.WithDescription("Leave a WhatsApp group by its ID."),
		mcp.WithTitleAnnotation("Leave Group"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleLeaveGroup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.LeaveGroup(ctx, domainGroup.LeaveGroupRequest{GroupID: trimmed}); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Left group %s", trimmed)), nil
}

func (h *GroupHandler) toolGetParticipants() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_participants",
		mcp.WithDescription("Retrieve the participant list for a group."),
		mcp.WithTitleAnnotation("List Participants"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleGetParticipants(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GetGroupParticipants(ctx, domainGroup.GetGroupParticipantsRequest{GroupID: trimmed})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Group %s has %d participants", resp.GroupID, len(resp.Participants))
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *GroupHandler) toolManageParticipants() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_manage_participants",
		mcp.WithDescription("Add, remove, promote, or demote group participants."),
		mcp.WithTitleAnnotation("Manage Participants"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithArray("participants",
			mcp.Description("Phone numbers of participants to modify (without suffix)."),
			mcp.Required(),
			mcp.WithStringItems(),
		),
		mcp.WithString("action",
			mcp.Description("Participant action: add, remove, promote, or demote."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleManageParticipants(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	var participants []string

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("participants are required")
	}

	rawParticipants, exists := args["participants"]
	if !exists {
		return nil, fmt.Errorf("participants are required")
	}

	participants, err = toStringSlice(rawParticipants)
	if err != nil {
		return nil, err
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants cannot be empty")
	}

	actionStr, err := request.RequireString("action")
	if err != nil {
		return nil, err
	}

	change, err := parseParticipantChange(actionStr)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	result, err := h.groupService.ManageParticipant(ctx, domainGroup.ParticipantRequest{
		GroupID:      trimmed,
		Participants: participants,
		Action:       change,
	})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Applied %s to %d participants in %s", strings.ToLower(actionStr), len(participants), trimmed)
	return mcp.NewToolResultStructured(result, fallback), nil
}

func (h *GroupHandler) toolGetInviteLink() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_invite_link",
		mcp.WithDescription("Fetch the invite link for a group, optionally resetting it."),
		mcp.WithTitleAnnotation("Get Invite Link"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithBoolean("reset",
			mcp.Description("If true, reset the invite link."),
			mcp.DefaultBool(false),
		),
	)
}

func (h *GroupHandler) handleGetInviteLink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	reset := false
	if rawArgs := request.GetArguments(); rawArgs != nil {
		if val, ok := rawArgs["reset"]; ok {
			parsed, err := toBool(val)
			if err != nil {
				return nil, err
			}
			reset = parsed
		}
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GetGroupInviteLink(ctx, domainGroup.GetGroupInviteLinkRequest{
		GroupID: trimmed,
		Reset:   reset,
	})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Invite link for %s: %s", trimmed, resp.InviteLink)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *GroupHandler) toolGroupInfo() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_info",
		mcp.WithDescription("Retrieve detailed WhatsApp group information."),
		mcp.WithTitleAnnotation("Group Info"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleGroupInfo(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GroupInfo(ctx, domainGroup.GroupInfoRequest{GroupID: trimmed})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Fetched group info for %s", trimmed)
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *GroupHandler) toolSetGroupName() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_name",
		mcp.WithDescription("Update the group's display name."),
		mcp.WithTitleAnnotation("Set Group Name"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithString("name",
			mcp.Description("New group name."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupName(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	name, err := request.RequireString("name")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupName(ctx, domainGroup.SetGroupNameRequest{GroupID: trimmed, Name: strings.TrimSpace(name)}); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Updated group %s name to %s", trimmed, strings.TrimSpace(name))), nil
}

func (h *GroupHandler) toolSetGroupTopic() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_topic",
		mcp.WithDescription("Update the group's topic or description."),
		mcp.WithTitleAnnotation("Set Group Topic"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithString("topic",
			mcp.Description("New group topic/description."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupTopic(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	topic, err := request.RequireString("topic")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupTopic(ctx, domainGroup.SetGroupTopicRequest{GroupID: trimmed, Topic: strings.TrimSpace(topic)}); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText(fmt.Sprintf("Updated group %s topic", trimmed)), nil
}

func (h *GroupHandler) toolSetGroupLocked() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_locked",
		mcp.WithDescription("Toggle whether only admins can edit group info."),
		mcp.WithTitleAnnotation("Set Group Locked"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithBoolean("locked",
			mcp.Description("Set to true to lock the group."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupLocked(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("locked flag is required")
	}

	val, ok := args["locked"]
	if !ok {
		return nil, fmt.Errorf("locked flag is required")
	}

	locked, err := toBool(val)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupLocked(ctx, domainGroup.SetGroupLockedRequest{GroupID: trimmed, Locked: locked}); err != nil {
		return nil, err
	}

	state := "unlocked"
	if locked {
		state = "locked"
	}

	return mcp.NewToolResultText(fmt.Sprintf("Group %s is now %s", trimmed, state)), nil
}

func (h *GroupHandler) toolSetGroupAnnounce() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_announce",
		mcp.WithDescription("Toggle announcement-only mode."),
		mcp.WithTitleAnnotation("Set Group Announce"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithBoolean("announce",
			mcp.Description("Set to true to allow only admins to send messages."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupAnnounce(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("announce flag is required")
	}

	val, ok := args["announce"]
	if !ok {
		return nil, fmt.Errorf("announce flag is required")
	}

	announce, err := toBool(val)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupAnnounce(ctx, domainGroup.SetGroupAnnounceRequest{GroupID: trimmed, Announce: announce}); err != nil {
		return nil, err
	}

	state := "regular chat"
	if announce {
		state = "announcement-only"
	}

	return mcp.NewToolResultText(fmt.Sprintf("Group %s is now in %s mode", trimmed, state)), nil
}

func (h *GroupHandler) toolListGroupJoinRequests() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_join_requests",
		mcp.WithDescription("List pending requests to join a group."),
		mcp.WithTitleAnnotation("List Join Requests"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleListGroupJoinRequests(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GetGroupRequestParticipants(ctx, domainGroup.GetGroupRequestParticipantsRequest{GroupID: trimmed})
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Group %s has %d pending requests", trimmed, len(resp))
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func (h *GroupHandler) toolManageGroupJoinRequests() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_manage_join_requests",
		mcp.WithDescription("Approve or reject pending group join requests."),
		mcp.WithTitleAnnotation("Manage Join Requests"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithArray("participants",
			mcp.Description("Phone numbers of requesters (without suffix)."),
			mcp.Required(),
			mcp.WithStringItems(),
		),
		mcp.WithString("action",
			mcp.Description("Action to apply: approve or reject."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleManageGroupJoinRequests(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("participants are required")
	}

	participantsRaw, ok := args["participants"]
	if !ok {
		return nil, fmt.Errorf("participants are required")
	}

	participants, err := toStringSlice(participantsRaw)
	if err != nil {
		return nil, err
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants cannot be empty")
	}

	actionStr, err := request.RequireString("action")
	if err != nil {
		return nil, err
	}

	change, err := parseParticipantRequestChange(actionStr)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	result, err := h.groupService.ManageGroupRequestParticipants(ctx, domainGroup.GroupRequestParticipantsRequest{
		GroupID:      trimmed,
		Participants: participants,
		Action:       change,
	})
	if err != nil {
		return nil, err
	}

	actionLower := strings.ToLower(actionStr)
	actionVerb := actionLower
	switch actionLower {
	case "approve":
		actionVerb = "approved"
	case "reject":
		actionVerb = "rejected"
	}

	actionReadable := actionVerb
	if len(actionVerb) > 0 {
		actionReadable = strings.ToUpper(actionVerb[:1]) + actionVerb[1:]
	}

	fallback := fmt.Sprintf("%s %d pending requests for %s", actionReadable, len(participants), trimmed)
	return mcp.NewToolResultStructured(result, fallback), nil
}

func parseParticipantChange(action string) (whatsmeow.ParticipantChange, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "add":
		return whatsmeow.ParticipantChangeAdd, nil
	case "remove":
		return whatsmeow.ParticipantChangeRemove, nil
	case "promote":
		return whatsmeow.ParticipantChangePromote, nil
	case "demote":
		return whatsmeow.ParticipantChangeDemote, nil
	default:
		return whatsmeow.ParticipantChange(""), fmt.Errorf("invalid participant action: %s", action)
	}
}

func parseParticipantRequestChange(action string) (whatsmeow.ParticipantRequestChange, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve":
		return whatsmeow.ParticipantChangeApprove, nil
	case "reject":
		return whatsmeow.ParticipantChangeReject, nil
	default:
		return whatsmeow.ParticipantRequestChange(""), fmt.Errorf("invalid join request action: %s", action)
	}
}

func toStringSlice(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}

	switch v := raw.(type) {
	case []string:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = strings.TrimSpace(item)
		}
		return result, nil
	case []any:
		result := make([]string, len(v))
		for i, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("participants[%d] is not a string", i)
			}
			result[i] = strings.TrimSpace(str)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("participants must be an array of strings")
	}
}
