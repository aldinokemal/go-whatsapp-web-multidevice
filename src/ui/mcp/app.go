package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type AppHandler struct {
	appService domainApp.IAppUsecase
}

func InitMcpApp(appService domainApp.IAppUsecase) *AppHandler {
	return &AppHandler{appService: appService}
}

func (h *AppHandler) AddAppTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolConnectionStatus(), h.handleConnectionStatus)
	mcpServer.AddTool(h.toolLoginWithQR(), h.handleLoginWithQR)
	mcpServer.AddTool(h.toolLoginWithCode(), h.handleLoginWithCode)
	mcpServer.AddTool(h.toolLogout(), h.handleLogout)
	mcpServer.AddTool(h.toolReconnect(), h.handleReconnect)
}

func (h *AppHandler) toolConnectionStatus() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_connection_status",
		mcp.WithDescription("Check whether the WhatsApp client is connected and logged in."),
		mcp.WithTitleAnnotation("Connection Status"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
}

func (h *AppHandler) handleConnectionStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	isConnected, isLoggedIn, deviceID := whatsapp.GetConnectionStatus()

	structured := map[string]any{
		"is_connected": isConnected,
		"is_logged_in": isLoggedIn,
	}
	if deviceID != "" {
		structured["device_id"] = deviceID
	}

	fallback := fmt.Sprintf("connected=%t logged_in=%t", isConnected, isLoggedIn)
	return mcp.NewToolResultStructured(structured, fallback), nil
}

func (h *AppHandler) toolLoginWithQR() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_login_qr",
		mcp.WithDescription("Initiate a QR code based login flow. Returns the QR image and pairing code."),
		mcp.WithTitleAnnotation("Login With QR"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
	)
}

func (h *AppHandler) handleLoginWithQR(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	resp, err := h.appService.Login(ctx)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf("Scan the QR image to log in (expires in ~%d seconds)", int(resp.Duration.Seconds()))
	structured := map[string]any{
		"qr_image_path": resp.ImagePath,
		"qr_code":       resp.Code,
		"expires_in":    int(resp.Duration.Seconds()),
	}

	qrBytes, readErr := os.ReadFile(resp.ImagePath)
	if readErr != nil {
		return mcp.NewToolResultStructured(structured, fallback), nil
	}

	encoded := base64.StdEncoding.EncodeToString(qrBytes)
	result := mcp.NewToolResultImage(fallback, encoded, "image/png")
	result.StructuredContent = structured
	return result, nil
}

func (h *AppHandler) toolLoginWithCode() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_login_with_code",
		mcp.WithDescription("Generate a pairing code for WhatsApp multi-device login using a phone number."),
		mcp.WithTitleAnnotation("Login With Pairing Code"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Description("Phone number in international format (e.g. +628123456789)."),
			mcp.Required(),
		),
	)
}

func (h *AppHandler) handleLoginWithCode(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	trimmedPhone := strings.TrimSpace(phone)
	pairCode, err := h.appService.LoginWithCode(ctx, trimmedPhone)
	if err != nil {
		return nil, err
	}

	structured := map[string]any{
		"phone":     trimmedPhone,
		"pair_code": pairCode,
	}

	fallback := fmt.Sprintf("Pair code %s generated for %s", pairCode, trimmedPhone)
	return mcp.NewToolResultStructured(structured, fallback), nil
}

func (h *AppHandler) toolLogout() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_logout",
		mcp.WithDescription("Sign out the current WhatsApp session and clear stored credentials."),
		mcp.WithTitleAnnotation("Logout"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
	)
}

func (h *AppHandler) handleLogout(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := h.appService.Logout(ctx); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText("Logged out successfully"), nil
}

func (h *AppHandler) toolReconnect() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_reconnect",
		mcp.WithDescription("Attempt to reconnect to WhatsApp using the stored session."),
		mcp.WithTitleAnnotation("Reconnect"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
}

func (h *AppHandler) handleReconnect(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := h.appService.Reconnect(ctx); err != nil {
		return nil, err
	}

	return mcp.NewToolResultText("Reconnect initiated"), nil
}
