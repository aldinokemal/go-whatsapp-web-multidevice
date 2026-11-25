package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type UserHandler struct {
	userService domainUser.IUserUsecase
	resolver    deviceResolver
}

func InitMcpUser(userService domainUser.IUserUsecase, resolver deviceResolver) *UserHandler {
	return &UserHandler{userService: userService, resolver: resolver}
}

func (h *UserHandler) AddUserTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_user",
		mcpg.WithDescription("Look up WhatsApp users and this account: info, check (is the number on WhatsApp), business_profile, avatar, subscribe_presence, set_push_name (change this account's display name), my_privacy, my_groups, my_newsletters, my_contacts_detailed."),
		mcpg.WithTitleAnnotation("User and Account"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(true),
		mcpg.WithIdempotentHintAnnotation(true),
		mcpg.WithRawInputSchema(json.RawMessage(userSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handleUser)
}

func (h *UserHandler) handleUser(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	// Same as REST /user/*: a bare number gets @s.whatsapp.net.
	phone := strings.TrimSpace(request.GetString("phone", ""))
	utils.SanitizePhone(&phone)

	var resp any
	switch action {
	case "info":
		resp, err = h.userService.Info(ctx, domainUser.InfoRequest{Phone: phone})
	case "check":
		resp, err = h.userService.IsOnWhatsApp(ctx, domainUser.CheckRequest{Phone: phone})
	case "business_profile":
		resp, err = h.userService.BusinessProfile(ctx, domainUser.BusinessProfileRequest{Phone: phone})
	case "avatar":
		resp, err = h.userService.Avatar(ctx, domainUser.AvatarRequest{
			Phone:       phone,
			IsPreview:   request.GetBool("is_preview", false),
			IsCommunity: request.GetBool("is_community", false),
		})
	case "subscribe_presence":
		resp, err = h.userService.SubscribePresence(ctx, domainUser.SubscribePresenceRequest{Phone: phone})
	case "set_push_name":
		pushName := request.GetString("push_name", "")
		if err := h.userService.ChangePushName(ctx, domainUser.ChangePushNameRequest{PushName: pushName}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Push name changed to %q", pushName)), nil
	case "my_privacy":
		resp, err = h.userService.MyPrivacySetting(ctx)
	case "my_groups":
		resp, err = h.userService.MyListGroups(ctx)
	case "my_newsletters":
		resp, err = h.userService.MyListNewsletter(ctx)
	case "my_contacts_detailed":
		// Optional capability, as for REST GET /user/my/contacts/detailed.
		detailed, ok := h.userService.(domainUser.IUserContactsDetailed)
		if !ok {
			return mcpg.NewToolResultError("my_contacts_detailed is not supported by this server"), nil
		}
		resp, err = detailed.MyListContactsDetailed(ctx)
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown user action: %s", action)), nil
	}
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return mcpg.NewToolResultStructuredOnly(resp), nil
}
