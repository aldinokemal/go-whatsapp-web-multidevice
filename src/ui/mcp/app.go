package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type AppHandler struct {
	appService domainApp.IAppUsecase
	resolver   deviceResolver
}

func InitMcpApp(appService domainApp.IAppUsecase, resolver deviceResolver) *AppHandler {
	return &AppHandler{appService: appService, resolver: resolver}
}

func (h *AppHandler) AddAppTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_app",
		mcpg.WithDescription("WhatsApp connection and session management: status, login_qr (returns QR image), login_code (pairing code for a phone number), logout, reconnect."),
		mcpg.WithTitleAnnotation("Connection & Session"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(true),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithRawInputSchema(json.RawMessage(appSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handleApp)
}

func (h *AppHandler) handleApp(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, inst, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	deviceID := inst.ID()

	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	switch action {
	case "status":
		isConnected, isLoggedIn, err := h.appService.Status(ctx, deviceID)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		structured := map[string]any{"is_connected": isConnected, "is_logged_in": isLoggedIn, "device_id": deviceID}
		return mcpg.NewToolResultStructured(structured, fmt.Sprintf("connected=%t logged_in=%t", isConnected, isLoggedIn)), nil
	case "login_qr":
		resp, err := h.appService.Login(ctx, deviceID)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		fallback := fmt.Sprintf("Scan the QR image to log in (expires in ~%d seconds)", int(resp.Duration.Seconds()))
		structured := map[string]any{
			"device_id":     deviceID,
			"qr_image_path": resp.ImagePath,
			"qr_code":       resp.Code,
			"expires_in":    int(resp.Duration.Seconds()),
		}
		qrBytes, readErr := os.ReadFile(resp.ImagePath)
		if readErr != nil {
			return mcpg.NewToolResultStructured(structured, fmt.Sprintf("%s. QR image unavailable: %v", fallback, readErr)), nil
		}
		result := mcpg.NewToolResultImage(fallback, base64.StdEncoding.EncodeToString(qrBytes), "image/png")
		result.StructuredContent = structured
		return result, nil
	case "login_code":
		phone := strings.TrimSpace(request.GetString("phone", ""))
		pairCode, err := h.appService.LoginWithCode(ctx, deviceID, phone)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		structured := map[string]any{"device_id": deviceID, "phone": phone, "pair_code": pairCode}
		return mcpg.NewToolResultStructured(structured, fmt.Sprintf("Pair code %s generated for %s", pairCode, phone)), nil
	case "logout":
		if err := h.appService.Logout(ctx, deviceID); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Logged out device %s successfully", deviceID)), nil
	case "reconnect":
		if err := h.appService.Reconnect(ctx, deviceID); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Reconnect initiated for %s", deviceID)), nil
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown app action: %s", action)), nil
	}
}
