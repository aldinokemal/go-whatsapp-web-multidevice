package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type NewsletterHandler struct {
	newsletterService domainNewsletter.INewsletterUsecase
	resolver          deviceResolver
}

func InitMcpNewsletter(newsletterService domainNewsletter.INewsletterUsecase, resolver deviceResolver) *NewsletterHandler {
	return &NewsletterHandler{newsletterService: newsletterService, resolver: resolver}
}

func (h *NewsletterHandler) AddNewsletterTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_newsletter",
		mcpg.WithDescription("WhatsApp Channels (newsletters): follow, unfollow, info (by JID or invite link, invite works without following), directory (search/browse public channels), messages (history with view/forward/reaction counts, paginate with before), message_updates (counters only), live_updates_subscribe/live_updates_unsubscribe/live_updates_list (auto-renewed live counter updates delivered as newsletter.message webhooks)."),
		mcpg.WithTitleAnnotation("Channels"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(false),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithRawInputSchema(json.RawMessage(newsletterSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, h.handleNewsletter)
}

func (h *NewsletterHandler) handleNewsletter(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, h.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	action, err := request.RequireString("action")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	newsletterID := strings.TrimSpace(request.GetString("newsletter_id", ""))

	switch action {
	case "follow":
		if err := h.newsletterService.Follow(ctx, domainNewsletter.FollowRequest{NewsletterID: newsletterID}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Followed newsletter %s", newsletterID)), nil
	case "unfollow":
		if err := h.newsletterService.Unfollow(ctx, domainNewsletter.UnfollowRequest{NewsletterID: newsletterID}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Unfollowed newsletter %s", newsletterID)), nil
	case "info":
		resp, err := h.newsletterService.GetInfo(ctx, domainNewsletter.GetInfoRequest{
			NewsletterID: newsletterID,
			Invite:       strings.TrimSpace(request.GetString("invite", "")),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "directory":
		resp, err := h.newsletterService.Directory(ctx, domainNewsletter.DirectoryRequest{
			View:         request.GetString("view", ""),
			SearchText:   request.GetString("search_text", ""),
			CountryCodes: request.GetStringSlice("country_codes", nil),
			Categories:   request.GetStringSlice("categories", nil),
			Limit:        request.GetInt("limit", 0),
			Cursor:       request.GetString("cursor", ""),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "messages":
		resp, err := h.newsletterService.GetMessages(ctx, domainNewsletter.GetMessagesRequest{
			NewsletterID: newsletterID,
			Count:        request.GetInt("count", 0),
			Before:       request.GetInt("before", 0),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "message_updates":
		resp, err := h.newsletterService.GetMessageUpdates(ctx, domainNewsletter.GetMessageUpdatesRequest{
			NewsletterID: newsletterID,
			Count:        request.GetInt("count", 0),
			Since:        int64(request.GetInt("since", 0)),
			After:        request.GetInt("after", 0),
		})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "live_updates_subscribe":
		resp, err := h.newsletterService.SubscribeLiveUpdates(ctx, domainNewsletter.LiveUpdatesRequest{NewsletterID: newsletterID})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructured(resp, fmt.Sprintf("Subscribed to live updates of %s (renewed automatically, current grant %ds)", newsletterID, resp.DurationSeconds)), nil
	case "live_updates_unsubscribe":
		if err := h.newsletterService.UnsubscribeLiveUpdates(ctx, domainNewsletter.LiveUpdatesRequest{NewsletterID: newsletterID}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Stopped renewing live updates of %s", newsletterID)), nil
	case "live_updates_list":
		resp, err := h.newsletterService.ListLiveUpdates(ctx)
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown newsletter action: %s", action)), nil
	}
}
