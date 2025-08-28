package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// AIBridge integrates the Python AI service with the Go MCP server.
type AIBridge struct {
	aiServiceURL string
	httpClient   *http.Client
	logger       *logrus.Logger
}

// AIRequest represents a request to the AI service.
type AIRequest struct {
	Message      Message      `json:"message"`
	Conversation Conversation `json:"conversation"`
	UserProfile  interface{}  `json:"user_profile,omitempty"`
	GroupContext interface{}  `json:"group_context,omitempty"`
}

// Message represents a WhatsApp message for AI processing.
type Message struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	SenderID   string    `json:"sender_id"`
	ChatID     string    `json:"chat_id"`
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"message_type"`
	IsGroup    bool      `json:"is_group"`
	GroupName  string    `json:"group_name,omitempty"`
	ReplyTo    string    `json:"reply_to,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// Conversation represents a chat conversation.
type Conversation struct {
	ChatID         string    `json:"chat_id"`
	Messages       []Message `json:"messages"`
	LastUpdated    time.Time `json:"last_updated"`
	Context        string    `json:"context"`
	IsGroup        bool      `json:"is_group"`
	GroupName      string    `json:"group_name,omitempty"`
	Participants   []string  `json:"participants"`
	AIEnabled      bool      `json:"ai_enabled"`
	MaxContextLength int     `json:"max_context_length"`
}

// AIResponse represents the AI's response.
type AIResponse struct {
	Text            string   `json:"text"`
	ShouldReply     bool     `json:"should_reply"`
	Questions       []string `json:"questions"`
	Context         string   `json:"context"`
	Confidence      float64  `json:"confidence"`
	Reasoning       string   `json:"reasoning,omitempty"`
	SuggestedActions []string `json:"suggested_actions"`
}

// ProcessMessageResponse represents the response from AI service.
type ProcessMessageResponse struct {
	Success      bool         `json:"success"`
	AIResponse   *AIResponse  `json:"ai_response"`
	Conversation *Conversation `json:"conversation"`
	Error        string       `json:"error,omitempty"`
}

// NewAIBridge creates a new AI bridge instance with configurable timeout and logging.
func NewAIBridge(aiServiceURL string, timeout time.Duration, logger *logrus.Logger) *AIBridge {
	if logger == nil {
		logger = logrus.New()
		logger.SetFormatter(&logrus.JSONFormatter{})
		logger.SetLevel(logrus.InfoLevel)
	}

	return &AIBridge{
		aiServiceURL: aiServiceURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
		logger: logger,
	}
}

// AddAITools adds AI-related tools to the MCP server.
func (b *AIBridge) AddAITools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(b.toolProcessMessage(), b.handleProcessMessage)
}

// toolProcessMessage creates the MCP tool for processing messages with AI.
func (b *AIBridge) toolProcessMessage() mcp.Tool {
	return mcp.NewTool("ai_process_message",
		mcp.WithDescription("Process a WhatsApp message with AI to generate intelligent responses."),
		mcp.WithString("message_id", mcp.Required(), mcp.Description("Unique message ID")),
		mcp.WithString("message_text", mcp.Required(), mcp.Description("Text content of the message")),
		mcp.WithString("sender_id", mcp.Required(), mcp.Description("Sender's phone number or ID")),
		mcp.WithString("chat_id", mcp.Required(), mcp.Description("Chat/group ID")),
		mcp.WithBoolean("is_group", mcp.Description("Whether this is a group message")),
		mcp.WithString("group_name", mcp.Description("Group name if it's a group message")),
		mcp.WithString("message_type", mcp.Default("text"), mcp.Description("Type of message (e.g., text, image)")),
		mcp.WithString("reply_to", mcp.Description("ID of message being replied to")),
		mcp.WithObject("metadata", mcp.Description("Additional message metadata")),
	)
}

// handleProcessMessage handles the AI message processing request.
func (b *AIBridge) handleProcessMessage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := request.GetArguments()

	// Validate required fields
	for _, key := range []string{"message_id", "message_text", "sender_id", "chat_id"} {
		if _, ok := args[key]; !ok || args[key] == "" {
			b.logger.WithField("key", key).Error("Missing or empty required argument")
			return nil, fmt.Errorf("missing required argument: %s", key)
		}
	}

	// Extract arguments with type safety
	messageID := args["message_id"].(string)
	messageText := args["message_text"].(string)
	senderID := args["sender_id"].(string)
	chatID := args["chat_id"].(string)
	isGroup, _ := args["is_group"].(bool)
	groupName, _ := args["group_name"].(string)
	messageType, _ := args["message_type"].(string)
	if messageType == "" {
		messageType = "text"
	}
	replyTo, _ := args["reply_to"].(string)
	metadata, _ := args["metadata"].(map[string]interface{})

	// Create message for AI processing
	message := Message{
		ID:        messageID,
		Text:      messageText,
		SenderID:  senderID,
		ChatID:    chatID,
		Timestamp: time.Now().UTC(),
		Type:      messageType,
		IsGroup:   isGroup,
		GroupName: groupName,
		ReplyTo:   replyTo,
		Metadata:  metadata,
	}

	// Create or update conversation
	conversation := Conversation{
		ChatID:         chatID,
		IsGroup:        isGroup,
		GroupName:      groupName,
		LastUpdated:    time.Now().UTC(),
		Messages:       []Message{message},
		Participants:   []string{senderID},
		AIEnabled:      true,
		MaxContextLength: 50,
	}

	// Prepare AI request
	aiRequest := AIRequest{
		Message:      message,
		Conversation: conversation,
	}

	// Send request to AI service
	response, err := b.sendAIRequest(ctx, aiRequest)
	if err != nil {
		b.logger.WithError(err).WithField("chat_id", chatID).Error("Failed to process AI request")
		return nil, fmt.Errorf("AI service error: %v", err)
	}

	if !response.Success {
		b.logger.WithField("error", response.Error).WithField("chat_id", chatID).Error("AI processing failed")
		return nil, fmt.Errorf("AI processing failed: %s", response.Error)
	}

	// Build result
	result := map[string]interface{}{
		"success":       true,
		"ai_response":   response.AIResponse,
		"conversation":  response.Conversation,
		"response_text": response.AIResponse.Text,
		"should_reply":  response.AIResponse.ShouldReply,
		"confidence":    response.AIResponse.Confidence,
		"questions":     response.AIResponse.Questions,
		"reasoning":     response.AIResponse.Reasoning,
	}

	b.logger.WithFields(logrus.Fields{
		"chat_id":    chatID,
		"message_id": messageID,
		"confidence": response.AIResponse.Confidence,
	}).Info("Successfully processed message with AI")

	return mcp.NewToolResultJSON(result), nil
}

// sendAIRequest sends a request to the AI service.
func (b *AIBridge) sendAIRequest(ctx context.Context, request AIRequest) (*ProcessMessageResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		b.logger.WithError(err).Error("Failed to marshal AI request")
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	url := fmt.Sprintf("%s/api/process-message", b.aiServiceURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		b.logger.WithError(err).Error("Failed to create AI request")
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Add API key if configured
	if apiKey := getAPIKey(); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.logger.WithError(err).WithField("url", url).Error("Failed to send AI request")
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"url":         url,
		}).Error("AI service returned non-200 status")
		return nil, fmt.Errorf("AI service returned status: %d", resp.StatusCode)
	}

	var response ProcessMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		b.logger.WithError(err).Error("Failed to decode AI response")
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	return &response, nil
}

// HealthCheck checks if the AI service is healthy.
func (b *AIBridge) HealthCheck(ctx context.Context) bool {
	url := fmt.Sprintf("%s/health", b.aiServiceURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		b.logger.WithError(err).Error("Failed to create health check request")
		return false
	}

	if apiKey := getAPIKey(); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.logger.WithError(err).WithField("url", url).Error("AI service health check failed")
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b.logger.WithField("status_code", resp.StatusCode).Error("AI service health check returned non-200 status")
		return false
	}

	b.logger.Info("AI service health check passed")
	return true
}

// getAPIKey retrieves the API key from environment variables.
func getAPIKey() string {
	return os.Getenv("AI_API_KEY")
}