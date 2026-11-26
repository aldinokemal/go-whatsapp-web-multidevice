package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type LabelHandler struct {
	labelService domainLabel.ILabelUsecase
	resolver     deviceResolver
}

func InitMcpLabel(labelService domainLabel.ILabelUsecase, resolver deviceResolver) *LabelHandler {
	return &LabelHandler{labelService: labelService, resolver: resolver}
}

func (h *LabelHandler) AddLabelTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_label",
		mcpg.WithDescription("Manage WhatsApp labels (Business accounts) and chat lists (personal accounts, type CUSTOM): list (labels and their chats as stored from app state), resync (fetch them all from WhatsApp again), edit (create, rename or delete a label), chat (add a chat to a label or remove it)."),
		mcpg.WithTitleAnnotation("Labels and Lists"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(true),
		mcpg.WithIdempotentHintAnnotation(true),
		mcpg.WithRawInputSchema(json.RawMessage(labelSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handleLabel)
}

// optionalInt32 returns the argument as a pointer, or nil when it is absent.
// A value outside the int32 range is an error instead of wrapping around.
func optionalInt32(request mcpg.CallToolRequest, name string) (*int32, error) {
	args := request.GetArguments()
	if args == nil {
		return nil, nil
	}
	if _, ok := args[name]; !ok {
		return nil, nil
	}
	value := request.GetInt(name, 0)
	if value < math.MinInt32 || value > math.MaxInt32 {
		return nil, fmt.Errorf("%s must be between 0 and %d", name, math.MaxInt32)
	}
	v := int32(value)
	return &v, nil
}

func (h *LabelHandler) handleLabel(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	switch action {
	case "list":
		resp, err := h.labelService.ListLabels(ctx)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "resync":
		resp, err := h.labelService.ResyncLabels(ctx)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "edit":
		color, err := optionalInt32(request, "color")
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		orderIndex, err := optionalInt32(request, "order_index")
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		resp, err := h.labelService.EditLabel(ctx, domainLabel.EditLabelRequest{
			LabelID:    request.GetString("label_id", ""),
			Name:       request.GetString("name", ""),
			Color:      color,
			Type:       request.GetString("type", ""),
			OrderIndex: orderIndex,
			Deleted:    request.GetBool("deleted", false),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, resp.Message), nil
	case "chat":
		// Same as whatsapp_chat: a bare number gets @s.whatsapp.net, a bare ID over 15 digits gets @g.us.
		chatJID := request.GetString("chat_jid", "")
		utils.SanitizePhone(&chatJID)
		var labeled *bool
		if args := request.GetArguments(); args != nil {
			if _, ok := args["labeled"]; ok {
				v := request.GetBool("labeled", false)
				labeled = &v
			}
		}
		resp, err := h.labelService.LabelChat(ctx, domainLabel.LabelChatRequest{
			ChatJID: chatJID,
			LabelID: request.GetString("label_id", ""),
			Labeled: labeled,
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, resp.Message), nil
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown label action: %s", action)), nil
	}
}
