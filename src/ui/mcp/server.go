package mcp

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/mark3labs/mcp-go/server"
)

// Deps carries the usecase instances the MCP tools call — the same instances
// the REST handlers hold, so both surfaces share one whatsmeow session.
type Deps struct {
	App      domainApp.IAppUsecase
	Send     domainSend.ISendUsecase
	Schedule domainSend.IScheduleUsecase
	Chat     domainChat.IChatUsecase
	User     domainUser.IUserUsecase
	Message  domainMessage.IMessageUsecase
	Group    domainGroup.IGroupUsecase
	// WTF: the usecases behind the REST routes upstream's MCP tools lacked
	// (see wtf_tools.go).
	Newsletter domainNewsletter.INewsletterUsecase
	Call       domainCall.ICallUsecase

	// WTF: ResolveDevice, when set, replaces the device manager for every
	// device lookup (the X-Device-Id header and a per-call device_id): it
	// gets the request context, which carries the signed-in user, so only
	// that user's devices resolve. Another user's device reads exactly like
	// an unknown one.
	ResolveDevice func(ctx context.Context, deviceID string) (*whatsapp.DeviceInstance, string, error)
	// WTF: ExtraTools, when set, adds tools to the server (wtf_my_devices).
	ExtraTools func(s *server.MCPServer)
	// WTF: ToolFilter, when set, decides which tools tools/list shows the
	// caller (admin tools are hidden from everyone else). It only hides:
	// every tool still checks its caller when it runs.
	ToolFilter server.ToolFilterFunc
}

// NewServer builds the MCPServer with the 6 consolidated tools registered.
func NewServer(deps Deps, resolver deviceResolver) *server.MCPServer {
	opts := []server.ServerOption{
		server.WithToolCapabilities(true),
		// Enforce the schemas' allOf/if/then conditionals at the mcp-go
		// layer, before any handler runs (SEP-1303). Without this,
		// inputValidator stays nil and the conditionals are advisory only.
		server.WithInputSchemaValidation(),
	}
	if deps.ToolFilter != nil { // WTF
		opts = append(opts, server.WithToolFilter(deps.ToolFilter))
	}
	s := server.NewMCPServer("WhatsApp Web Multidevice MCP Server", config.AppVersion, opts...)
	if deps.ResolveDevice != nil { // WTF: owner-checked lookups only
		resolver = hookResolver(deps.ResolveDevice)
	}
	InitMcpSend(deps.Send, resolver).AddSendTools(s)
	InitMcpSchedule(deps.Schedule, resolver).AddScheduleTools(s)
	InitMcpMessage(deps.Message, resolver).AddMessageTools(s)
	InitMcpChat(deps.Chat, deps.User, resolver).AddChatTools(s)
	InitMcpGroup(deps.Group, resolver).AddGroupTools(s)
	InitMcpApp(deps.App, resolver).AddAppTools(s)
	addWTFTools(s, deps, resolver) // WTF: the REST routes upstream's tools lacked
	if deps.ExtraTools != nil {    // WTF
		deps.ExtraTools(s)
	}
	return s
}
