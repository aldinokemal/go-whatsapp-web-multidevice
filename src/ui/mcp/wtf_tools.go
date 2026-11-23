package mcp

// WTF: tools for the upstream REST routes the consolidated tools lacked
// (house rule S5: whatever the API can do, MCP can do). Each one resolves
// its device exactly as the upstream tools do (resolveDeviceContext: the
// device_id argument, else the X-Device-Id header, else the caller's only
// number; in multi-user mode only the caller's own numbers resolve) and then
// calls the very usecase method the REST handler calls.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Names of the WTF tools this file adds.
const (
	ToolContactInfo   = "wtf_contact_info"
	ToolMyWhatsApp    = "wtf_my_whatsapp"
	ToolSetPushName   = "wtf_set_push_name"
	ToolChatSettings  = "wtf_chat_settings"
	ToolPresence      = "wtf_presence"
	ToolRejectCall    = "wtf_reject_call"
	ToolNewsletter    = "wtf_newsletter"
	ToolGroupFromLink = "wtf_group_preview"
)

const deviceIDProp = `"device_id": {"type": "string", "description": "Which of your WhatsApp numbers acts (from wtf_my_devices). Optional when you have exactly one number"}`

const contactInfoSchema = `{
  "type": "object",
  "required": ["action", "phone"],
  "properties": {
    "action": {"type": "string", "enum": ["info", "avatar", "check", "business_profile"], "description": "info = WhatsApp profile (status, devices, verified name); avatar = profile picture URL; check = is this number on WhatsApp; business_profile = a business account's profile"},
    "phone": {"type": "string", "description": "Phone number in international format, digits only or with +, e.g. 27825550100"},
    "is_preview": {"type": "boolean", "description": "avatar: the small preview picture instead of the full one (default false)"},
    "is_community": {"type": "boolean", "description": "avatar: the number is a community (default false)"},
    ` + deviceIDProp + `
  }
}`

const myWhatsAppSchema = `{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["privacy", "groups", "newsletters"], "description": "privacy = your WhatsApp privacy settings; groups = the groups you are in; newsletters = the channels you follow"},
    ` + deviceIDProp + `
  }
}`

const setPushNameSchema = `{
  "type": "object",
  "required": ["push_name"],
  "properties": {
    "push_name": {"type": "string", "minLength": 1, "description": "The display name other people see for this WhatsApp number"},
    ` + deviceIDProp + `
  }
}`

const chatSettingsSchema = `{
  "type": "object",
  "required": ["action", "chat_jid"],
  "properties": {
    "action": {"type": "string", "enum": ["pin", "disappearing", "request_history"], "description": "pin = pin or unpin a chat; disappearing = set the chat's disappearing-message timer; request_history = ask your phone for older messages of this chat"},
    "chat_jid": {"type": "string", "description": "Chat JID, e.g. 27825550100@s.whatsapp.net or 1203630...@g.us"},
    "pinned": {"type": "boolean", "description": "pin: true pins, false unpins"},
    "timer_seconds": {"type": "integer", "enum": [0, 86400, 604800, 7776000], "description": "disappearing: 0 = off, 86400 = 24 hours, 604800 = 7 days, 7776000 = 90 days"},
    "count": {"type": "integer", "minimum": 1, "maximum": 500, "description": "request_history: how many older messages to ask for (default 50)"},
    ` + deviceIDProp + `
  },
  "allOf": [
    {"if": {"properties": {"action": {"const": "pin"}}}, "then": {"required": ["pinned"]}},
    {"if": {"properties": {"action": {"const": "disappearing"}}}, "then": {"required": ["timer_seconds"]}}
  ]
}`

const presenceSchema = `{
  "type": "object",
  "required": ["action"],
  "properties": {
    "action": {"type": "string", "enum": ["presence", "chat_presence"], "description": "presence = show this number as available or unavailable to everyone; chat_presence = show or stop 'typing...' in one chat"},
    "type": {"type": "string", "enum": ["available", "unavailable"], "description": "presence: available or unavailable"},
    "phone": {"type": "string", "description": "chat_presence: the chat's phone number or group JID"},
    "state": {"type": "string", "enum": ["start", "stop"], "description": "chat_presence: start or stop typing"},
    ` + deviceIDProp + `
  },
  "allOf": [
    {"if": {"properties": {"action": {"const": "presence"}}}, "then": {"required": ["type"]}},
    {"if": {"properties": {"action": {"const": "chat_presence"}}}, "then": {"required": ["phone", "state"]}}
  ]
}`

const rejectCallSchema = `{
  "type": "object",
  "required": ["caller_jid", "call_id"],
  "properties": {
    "caller_jid": {"type": "string", "description": "JID of the person calling, from the call webhook event"},
    "call_id": {"type": "string", "description": "The call's id, from the call webhook event"},
    ` + deviceIDProp + `
  }
}`

const newsletterSchema = `{
  "type": "object",
  "required": ["action", "newsletter_id"],
  "properties": {
    "action": {"type": "string", "enum": ["messages", "download_media", "unfollow"], "description": "messages = the channel's latest posts; download_media = save one post's media and return where it is; unfollow = stop following the channel"},
    "newsletter_id": {"type": "string", "description": "The channel's JID, e.g. 1203630...@newsletter (from wtf_my_whatsapp action=newsletters)"},
    "count": {"type": "integer", "minimum": 1, "maximum": 100, "description": "messages: how many (default 50)"},
    "before": {"type": "integer", "minimum": 1, "description": "messages: only posts older than this server_id (paging back)"},
    "server_id": {"type": "integer", "minimum": 1, "description": "download_media: the post's server_id (from action=messages)"},
    ` + deviceIDProp + `
  },
  "allOf": [
    {"if": {"properties": {"action": {"const": "download_media"}}}, "then": {"required": ["server_id"]}}
  ]
}`

const groupPreviewSchema = `{
  "type": "object",
  "required": ["link"],
  "properties": {
    "link": {"type": "string", "description": "A WhatsApp group invite link, e.g. https://chat.whatsapp.com/AbC123"},
    ` + deviceIDProp + `
  }
}`

type wtfTools struct {
	deps     Deps
	resolver deviceResolver
}

func rawTool(name, title, desc, schema string, readOnly, destructive, idempotent bool) mcpg.Tool {
	tool := mcpg.NewTool(name,
		mcpg.WithDescription(desc),
		mcpg.WithTitleAnnotation(title),
		mcpg.WithReadOnlyHintAnnotation(readOnly),
		mcpg.WithDestructiveHintAnnotation(destructive),
		mcpg.WithIdempotentHintAnnotation(idempotent),
		mcpg.WithOpenWorldHintAnnotation(true), // they talk to WhatsApp
		mcpg.WithRawInputSchema(json.RawMessage(schema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set.
	tool.InputSchema = mcpg.ToolInputSchema{}
	return tool
}

// addWTFTools registers the tools for the REST routes upstream's lacked.
func addWTFTools(s *server.MCPServer, deps Deps, resolver deviceResolver) {
	t := &wtfTools{deps: deps, resolver: resolver}
	s.AddTool(rawTool(ToolContactInfo, "Look up a WhatsApp contact",
		"Looks up a phone number on WhatsApp: whether it is on WhatsApp (check), its profile (info), its profile picture URL (avatar) or its business profile (business_profile). Read-only.",
		contactInfoSchema, true, false, true), t.contactInfo)
	s.AddTool(rawTool(ToolMyWhatsApp, "My WhatsApp settings and lists",
		"Reads your own WhatsApp number's privacy settings, the groups it is in, or the channels (newsletters) it follows. Read-only. For contacts use whatsapp_chat action=list_contacts.",
		myWhatsAppSchema, true, false, true), t.myWhatsApp)
	s.AddTool(rawTool(ToolSetPushName, "Change my WhatsApp display name",
		"Changes the display name (push name) other people see for your WhatsApp number.",
		setPushNameSchema, false, false, true), t.setPushName)
	s.AddTool(rawTool(ToolChatSettings, "Chat settings",
		"Changes one chat: pin or unpin it, set its disappearing-message timer, or ask your phone to send older messages of it into chat storage (request_history; they arrive over the next minutes). For archive use whatsapp_chat action=archive.",
		chatSettingsSchema, false, false, false), t.chatSettings)
	s.AddTool(rawTool(ToolPresence, "Online and typing status",
		"Sets whether your number shows as available (online) or unavailable to everyone, or shows/stops 'typing...' in one chat.",
		presenceSchema, false, false, true), t.presence)
	s.AddTool(rawTool(ToolRejectCall, "Reject an incoming call",
		"Rejects an incoming WhatsApp call, using the caller JID and call id from the call webhook event.",
		rejectCallSchema, false, false, false), t.rejectCall)
	s.AddTool(rawTool(ToolNewsletter, "WhatsApp channels",
		"Works with one WhatsApp channel (newsletter) you follow: read its latest posts (messages), save one post's media and get where it is (download_media), or stop following it (unfollow, which cannot be undone from here).",
		newsletterSchema, false, true, false), t.newsletter)
	s.AddTool(rawTool(ToolGroupFromLink, "Preview a group from its invite link",
		"Shows a WhatsApp group's name, topic, size and settings from an invite link, without joining it. To join, use whatsapp_group action=join_with_link. Read-only.",
		groupPreviewSchema, true, false, true), t.groupPreview)
}

func (t *wtfTools) device(ctx context.Context, req mcpg.CallToolRequest) (context.Context, *mcpg.CallToolResult) {
	ctx, _, err := resolveDeviceContext(ctx, req, t.resolver)
	if err != nil {
		return ctx, mcpg.NewToolResultError(err.Error())
	}
	return ctx, nil
}

func structured(v any, text string) *mcpg.CallToolResult {
	return mcpg.NewToolResultStructured(v, text)
}

func (t *wtfTools) contactInfo(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	phone := strings.TrimSpace(req.GetString("phone", ""))
	action := req.GetString("action", "")
	if action != "check" { // as the REST routes do: /user/check takes the number as typed
		utils.SanitizePhone(&phone)
	}
	var (
		out any
		err error
	)
	switch action {
	case "info":
		out, err = t.deps.User.Info(ctx, domainUser.InfoRequest{Phone: phone})
	case "avatar":
		out, err = t.deps.User.Avatar(ctx, domainUser.AvatarRequest{Phone: phone, IsPreview: req.GetBool("is_preview", false), IsCommunity: req.GetBool("is_community", false)})
	case "check":
		out, err = t.deps.User.IsOnWhatsApp(ctx, domainUser.CheckRequest{Phone: phone})
	case "business_profile":
		out, err = t.deps.User.BusinessProfile(ctx, domainUser.BusinessProfileRequest{Phone: phone})
	default:
		return mcpg.NewToolResultError("unknown action: " + action), nil
	}
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return mcpg.NewToolResultStructuredOnly(out), nil
}

func (t *wtfTools) myWhatsApp(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	var (
		out any
		err error
	)
	switch action := req.GetString("action", ""); action {
	case "privacy":
		out, err = t.deps.User.MyPrivacySetting(ctx)
	case "groups":
		out, err = t.deps.User.MyListGroups(ctx)
	case "newsletters":
		out, err = t.deps.User.MyListNewsletter(ctx)
	default:
		return mcpg.NewToolResultError("unknown action: " + action), nil
	}
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return mcpg.NewToolResultStructuredOnly(out), nil
}

func (t *wtfTools) setPushName(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	name := req.GetString("push_name", "")
	if err := t.deps.User.ChangePushName(ctx, domainUser.ChangePushNameRequest{PushName: name}); err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return structured(map[string]any{"push_name": name}, "Display name changed"), nil
}

func (t *wtfTools) chatSettings(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	jid := strings.TrimSpace(req.GetString("chat_jid", ""))
	switch action := req.GetString("action", ""); action {
	case "pin":
		resp, err := t.deps.Chat.PinChat(ctx, domainChat.PinChatRequest{ChatJID: jid, Pinned: req.GetBool("pinned", false)})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return structured(resp, resp.Message), nil
	case "disappearing":
		resp, err := t.deps.Chat.SetDisappearingTimer(ctx, domainChat.SetDisappearingTimerRequest{ChatJID: jid, TimerSeconds: uint32(req.GetInt("timer_seconds", 0))})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return structured(resp, resp.Message), nil
	case "request_history":
		resp, err := t.deps.Chat.RequestChatHistory(ctx, domainChat.RequestChatHistoryRequest{ChatJID: jid, Count: req.GetInt("count", 50)})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return structured(resp, resp.Status), nil
	default:
		return mcpg.NewToolResultError("unknown action: " + action), nil
	}
}

func (t *wtfTools) presence(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	var (
		resp domainSend.GenericResponse
		err  error
	)
	switch action := req.GetString("action", ""); action {
	case "presence":
		resp, err = t.deps.Send.SendPresence(ctx, domainSend.PresenceRequest{Type: req.GetString("type", "")})
	case "chat_presence":
		phone := strings.TrimSpace(req.GetString("phone", ""))
		utils.SanitizePhone(&phone)
		resp, err = t.deps.Send.SendChatPresence(ctx, domainSend.ChatPresenceRequest{Phone: phone, Action: req.GetString("state", "")})
	default:
		return mcpg.NewToolResultError("unknown action: " + action), nil
	}
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return structured(resp, resp.Status), nil
}

func (t *wtfTools) rejectCall(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	if err := t.deps.Call.RejectCall(ctx, req.GetString("caller_jid", ""), req.GetString("call_id", "")); err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return mcpg.NewToolResultText("Call rejected"), nil
}

func (t *wtfTools) newsletter(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	id := strings.TrimSpace(req.GetString("newsletter_id", ""))
	switch action := req.GetString("action", ""); action {
	case "messages":
		resp, err := t.deps.Newsletter.GetMessages(ctx, domainNewsletter.GetMessagesRequest{NewsletterID: id, Count: req.GetInt("count", 0), Before: req.GetInt("before", 0)})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultStructuredOnly(resp), nil
	case "download_media":
		resp, err := t.deps.Newsletter.DownloadMedia(ctx, domainNewsletter.DownloadMediaRequest{NewsletterID: id, ServerID: req.GetInt("server_id", 0)})
		if err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return structured(resp, resp.Status), nil
	case "unfollow":
		if err := t.deps.Newsletter.Unfollow(ctx, domainNewsletter.UnfollowRequest{NewsletterID: id}); err != nil {
			return mcpg.NewToolResultError(err.Error()), nil
		}
		return mcpg.NewToolResultText(fmt.Sprintf("Unfollowed %s", id)), nil
	default:
		return mcpg.NewToolResultError("unknown action: " + action), nil
	}
}

func (t *wtfTools) groupPreview(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, bad := t.device(ctx, req)
	if bad != nil {
		return bad, nil
	}
	resp, err := t.deps.Group.GetGroupInfoFromLink(ctx, domainGroup.GetGroupInfoFromLinkRequest{Link: strings.TrimSpace(req.GetString("link", ""))})
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	return mcpg.NewToolResultStructuredOnly(resp), nil
}
