package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mau.fi/whatsmeow"
)

type GroupHandler struct {
	groupService domainGroup.IGroupUsecase
	resolver     deviceResolver
}

func InitMcpGroup(groupService domainGroup.IGroupUsecase, resolver deviceResolver) *GroupHandler {
	return &GroupHandler{groupService: groupService, resolver: resolver}
}

func (h *GroupHandler) AddGroupTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_group",
		mcpg.WithDescription("Manage WhatsApp groups: create, join_with_link, leave, info, participants, add/remove/promote/demote participants, invite_link, set_name, set_topic, set_settings (announce/locked), join_requests, manage_join_requests."),
		mcpg.WithTitleAnnotation("Group Management"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(true),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithRawInputSchema(json.RawMessage(groupSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handleGroup)
}

func (h *GroupHandler) handleGroup(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	groupID := strings.TrimSpace(request.GetString("group_id", ""))
	utils.SanitizePhone(&groupID)
	participants := trimAll(request.GetStringSlice("participants", nil))

	switch action {
	case "create":
		title := strings.TrimSpace(request.GetString("title", ""))
		newGroupID, err := h.groupService.CreateGroup(ctx, domainGroup.CreateGroupRequest{
			Title: title, Participants: participants,
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		structured := map[string]any{"group_id": newGroupID, "title": title, "members": len(participants)}
		return mcpg.NewToolResultStructured(structured, fmt.Sprintf("Created group %s with %d members", newGroupID, len(participants))), nil
	case "join_with_link":
		link := strings.TrimSpace(request.GetString("invite_link", ""))
		joinedID, err := h.groupService.JoinGroupWithLink(ctx, domainGroup.JoinGroupWithLinkRequest{Link: link})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		structured := map[string]any{"group_id": joinedID, "invite_link": link}
		return mcpg.NewToolResultStructured(structured, fmt.Sprintf("Joined group %s", joinedID)), nil
	case "leave":
		if err := h.groupService.LeaveGroup(ctx, domainGroup.LeaveGroupRequest{GroupID: groupID}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Left group %s", groupID)), nil
	case "info":
		resp, err := h.groupService.GroupInfo(ctx, domainGroup.GroupInfoRequest{GroupID: groupID})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Fetched group info for %s", groupID)), nil
	case "participants":
		resp, err := h.groupService.GetGroupParticipants(ctx, domainGroup.GetGroupParticipantsRequest{GroupID: groupID})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Group %s has %d participants", resp.GroupID, len(resp.Participants))), nil
	case "add_participants", "remove_participants", "promote", "demote":
		change := map[string]whatsmeow.ParticipantChange{
			"add_participants":    whatsmeow.ParticipantChangeAdd,
			"remove_participants": whatsmeow.ParticipantChangeRemove,
			"promote":             whatsmeow.ParticipantChangePromote,
			"demote":              whatsmeow.ParticipantChangeDemote,
		}[action]
		result, err := h.groupService.ManageParticipant(ctx, domainGroup.ParticipantRequest{
			GroupID: groupID, Participants: participants, Action: change,
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(result, fmt.Sprintf("Applied %s to %d participants in %s", action, len(participants), groupID)), nil
	case "invite_link":
		resp, err := h.groupService.GetGroupInviteLink(ctx, domainGroup.GetGroupInviteLinkRequest{
			GroupID: groupID, Reset: request.GetBool("reset", false),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Invite link for %s: %s", groupID, resp.InviteLink)), nil
	case "set_name":
		name := strings.TrimSpace(request.GetString("name", ""))
		if err := h.groupService.SetGroupName(ctx, domainGroup.SetGroupNameRequest{GroupID: groupID, Name: name}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Updated group %s name to %s", groupID, name)), nil
	case "set_topic":
		topic := strings.TrimSpace(request.GetString("topic", ""))
		if err := h.groupService.SetGroupTopic(ctx, domainGroup.SetGroupTopicRequest{GroupID: groupID, Topic: topic}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Updated group %s topic", groupID)), nil
	case "set_settings":
		args := request.GetArguments()
		applied := []string{}
		if _, ok := args["announce"]; ok {
			if err := h.groupService.SetGroupAnnounce(ctx, domainGroup.SetGroupAnnounceRequest{
				GroupID: groupID, Announce: request.GetBool("announce", false),
			}); err != nil {
				return mcpg.NewToolResultError(err.Error()), nil
			}
			applied = append(applied, "announce")
		}
		if _, ok := args["locked"]; ok {
			if err := h.groupService.SetGroupLocked(ctx, domainGroup.SetGroupLockedRequest{
				GroupID: groupID, Locked: request.GetBool("locked", false),
			}); err != nil {
				return mcpg.NewToolResultError(err.Error()), nil
			}
			applied = append(applied, "locked")
		}
		if len(applied) == 0 {
			return mcpg.NewToolResultError("set_settings requires announce and/or locked"), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Updated %s for group %s", strings.Join(applied, "+"), groupID)), nil
	case "join_requests":
		resp, err := h.groupService.GetGroupRequestParticipants(ctx, domainGroup.GetGroupRequestParticipantsRequest{GroupID: groupID})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Group %s has %d pending requests", groupID, len(resp))), nil
	case "manage_join_requests":
		change, err := parseParticipantRequestChange(request.GetString("request_action", ""))
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		result, err := h.groupService.ManageGroupRequestParticipants(ctx, domainGroup.GroupRequestParticipantsRequest{
			GroupID: groupID, Participants: participants, Action: change,
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(result, fmt.Sprintf("Applied %s to %d pending requests for %s", request.GetString("request_action", ""), len(participants), groupID)), nil
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown group action: %s", action)), nil
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

func trimAll(items []string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = strings.TrimSpace(item)
	}
	return out
}
