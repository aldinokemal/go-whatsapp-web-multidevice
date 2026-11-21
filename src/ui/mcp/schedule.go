package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type ScheduleHandler struct {
	service  domainSend.IScheduleUsecase
	resolver deviceResolver
}

func InitMcpSchedule(service domainSend.IScheduleUsecase, resolver deviceResolver) *ScheduleHandler {
	return &ScheduleHandler{service: service, resolver: resolver}
}

func (h *ScheduleHandler) AddScheduleTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_schedule",
		mcpg.WithDescription("List and manage scheduled WhatsApp sends. Actions: list, get, pause, resume, cancel. Only an active schedule can be paused, only a paused one resumed, and active, paused, or failed ones cancelled."),
		mcpg.WithTitleAnnotation("Scheduled Sends"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(true),
		mcpg.WithRawInputSchema(json.RawMessage(scheduleSchema)),
	)
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handle)
}

func (h *ScheduleHandler) handle(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	id := request.GetString("schedule_id", "")
	var done string
	switch action {
	case "list":
		result, err := h.service.List(ctx, domainSend.ScheduleFilter{
			Status:      request.GetString("status", ""),
			Search:      request.GetString("search", ""),
			MessageType: request.GetString("message_type", ""),
			Limit:       request.GetInt("limit", 25),
			Offset:      request.GetInt("offset", 0),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(result), nil
	case "get":
		item, err := h.service.Get(ctx, id)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(item), nil
	case "pause":
		err = h.service.Pause(ctx, id)
		done = "paused"
	case "resume":
		err = h.service.Resume(ctx, id)
		done = "resumed"
	case "cancel":
		err = h.service.Cancel(ctx, id)
		done = "cancelled"
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown schedule action: %s", action)), nil
	}
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return mcpg.NewToolResultText(fmt.Sprintf("Schedule %s %s", id, done)), nil
}
