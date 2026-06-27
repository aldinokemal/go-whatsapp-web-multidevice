package mcp

import (
	"context"
	"errors"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	mcpHelpers "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/helpers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type CallHandler struct {
	callService domainCall.ICallUsecase
}

func InitMcpCall(callService domainCall.ICallUsecase) *CallHandler {
	return &CallHandler{callService: callService}
}

func (h *CallHandler) AddCallTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolStartCall(), h.handleStartCall)
	mcpServer.AddTool(h.toolAcceptCall(), h.handleAcceptCall)
	mcpServer.AddTool(h.toolRejectCall(), h.handleRejectCall)
	mcpServer.AddTool(h.toolEndCall(), h.handleEndCall)
	mcpServer.AddTool(h.toolCallStatus(), h.handleCallStatus)
	mcpServer.AddTool(h.toolListCalls(), h.handleListCalls)
}

func (h *CallHandler) toolStartCall() mcp.Tool {
	return mcp.NewTool("whatsapp_call_start",
		mcp.WithDescription("Start a 1:1 WhatsApp voice call. Audio negotiation must be completed by a WebRTC-capable client."),
		mcp.WithString("phone", mcp.Required(), mcp.Description("International phone number without local leading zero.")),
	)
}

func (h *CallHandler) toolAcceptCall() mcp.Tool {
	return callIDTool("whatsapp_call_accept", "Accept an incoming WhatsApp voice call.")
}

func (h *CallHandler) toolRejectCall() mcp.Tool {
	return callIDTool("whatsapp_call_reject", "Reject an incoming WhatsApp voice call.")
}

func (h *CallHandler) toolEndCall() mcp.Tool {
	return callIDTool("whatsapp_call_end", "End an active WhatsApp voice call.")
}

func (h *CallHandler) toolCallStatus() mcp.Tool {
	return callIDTool("whatsapp_call_status", "Get the status of a WhatsApp voice call.")
}

func (h *CallHandler) toolListCalls() mcp.Tool {
	return mcp.NewTool("whatsapp_call_list",
		mcp.WithDescription("List WhatsApp voice call history for the selected/default device."),
		mcp.WithReadOnlyHintAnnotation(true),
	)
}

func callIDTool(name, description string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithString("call_id", mcp.Required(), mcp.Description("WhatsApp call ID.")),
	)
}

func (h *CallHandler) handleStartCall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}
	phone, ok := request.GetArguments()["phone"].(string)
	if !ok || phone == "" {
		return nil, errors.New("phone must be a string")
	}
	resp, err := h.callService.StartCall(ctx, domainCall.StartCallRequest{Phone: phone})
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultStructured(resp, "Call started"), nil
}

func (h *CallHandler) handleAcceptCall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return h.handleCallIDAction(ctx, request, h.callService.AcceptCall, "Call accepted")
}

func (h *CallHandler) handleRejectCall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return h.handleCallIDAction(ctx, request, h.callService.RejectCall, "Call rejected")
}

func (h *CallHandler) handleEndCall(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return h.handleCallIDAction(ctx, request, h.callService.EndCall, "Call ended")
}

func (h *CallHandler) handleCallStatus(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, req, err := callIDRequestFromMCP(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := h.callService.GetCall(ctx, req)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultStructured(resp, "Call found"), nil
}

func (h *CallHandler) handleListCalls(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := h.callService.ListCalls(ctx)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultStructured(resp, "Calls listed"), nil
}

func (h *CallHandler) handleCallIDAction(
	ctx context.Context,
	request mcp.CallToolRequest,
	action func(context.Context, domainCall.CallIDRequest) (domainCall.GenericResponse, error),
	fallback string,
) (*mcp.CallToolResult, error) {
	ctx, req, err := callIDRequestFromMCP(ctx, request)
	if err != nil {
		return nil, err
	}
	resp, err := action(ctx, req)
	if err != nil {
		return nil, err
	}
	return mcp.NewToolResultStructured(resp, fallback), nil
}

func callIDRequestFromMCP(ctx context.Context, request mcp.CallToolRequest) (context.Context, domainCall.CallIDRequest, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return ctx, domainCall.CallIDRequest{}, err
	}
	callID, ok := request.GetArguments()["call_id"].(string)
	if !ok || callID == "" {
		return ctx, domainCall.CallIDRequest{}, errors.New("call_id must be a string")
	}
	return ctx, domainCall.CallIDRequest{CallID: callID}, nil
}
