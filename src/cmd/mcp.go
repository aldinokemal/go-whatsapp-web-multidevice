package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/helpers"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start WhatsApp MCP server using SSE",
	Long:  `Start a WhatsApp MCP (Model Context Protocol) server using Server-Sent Events (SSE) transport. This allows AI agents to interact with WhatsApp through a standardized protocol.`,
	Run:   mcpServer,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.Flags().StringVar(&config.McpPort, "port", "8080", "Port for the SSE MCP server")
	mcpCmd.Flags().StringVar(&config.McpHost, "host", "localhost", "Host for the SSE MCP server")
}

func mcpServer(_ *cobra.Command, _ []string) {
	// Set auto reconnect to whatsapp server after booting
	go helpers.SetAutoConnectAfterBooting(appUsecase)
	// Set auto reconnect checking
	go helpers.SetAutoReconnectChecking(whatsappCli)

	// Create MCP server with capabilities
	mcpServer := server.NewMCPServer(
		"WhatsApp Web Multidevice MCP Server",
		config.AppVersion,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
	)

	// Add all WhatsApp tools
	sendHandler := mcp.InitMcpSend(sendUsecase)
	sendHandler.AddSendTools(mcpServer)

	// Add AI tools
	aiServiceURL := os.Getenv("AI_SERVICE_URL")
	if aiServiceURL == "" {
		aiServiceURL = "http://localhost:8000" // fallback for local development
	}
	aiBridge := mcp.NewAIBridge(aiServiceURL, 120*time.Second, logrus.StandardLogger())
	aiBridge.AddAITools(mcpServer)

	// Create SSE server
	sseServer := server.NewSSEServer(
		mcpServer,
		server.WithBaseURL(fmt.Sprintf("http://%s:%s", config.McpHost, config.McpPort)),
		server.WithKeepAlive(true),
	)

	// Start the SSE server
	addr := fmt.Sprintf("%s:%s", config.McpHost, config.McpPort)
	logrus.Printf("Starting WhatsApp MCP SSE server on %s", addr)
	logrus.Printf("SSE endpoint: http://%s:%s/sse", config.McpHost, config.McpPort)
	logrus.Printf("Message endpoint: http://%s:%s/message", config.McpHost, config.McpPort)

	if err := sseServer.Start(addr); err != nil {
		logrus.Fatalf("Failed to start SSE server: %v", err)
	}
}
