package mcp

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/mark3labs/mcp-go/server"
)

// Deps carries the usecase instances the MCP tools call — the same instances
// the REST handlers hold, so both surfaces share one whatsmeow session.
type Deps struct {
	App     domainApp.IAppUsecase
	Send    domainSend.ISendUsecase
	Chat    domainChat.IChatUsecase
	User    domainUser.IUserUsecase
	Message domainMessage.IMessageUsecase
	Group   domainGroup.IGroupUsecase
}

// NewServer builds the MCPServer with the 5 consolidated tools registered.
func NewServer(deps Deps, resolver deviceResolver) *server.MCPServer {
	s := server.NewMCPServer(
		"WhatsApp Web Multidevice MCP Server",
		config.AppVersion,
		server.WithToolCapabilities(true),
		// Enforce the schemas' allOf/if/then conditionals at the mcp-go
		// layer, before any handler runs (SEP-1303). Without this,
		// inputValidator stays nil and the conditionals are advisory only.
		server.WithInputSchemaValidation(),
	)
	InitMcpSend(deps.Send, resolver).AddSendTools(s)
	InitMcpMessage(deps.Message, resolver).AddMessageTools(s)
	InitMcpChat(deps.Chat, deps.User, resolver).AddChatTools(s)
	InitMcpGroup(deps.Group, resolver).AddGroupTools(s)
	InitMcpApp(deps.App, resolver).AddAppTools(s)
	return s
}
