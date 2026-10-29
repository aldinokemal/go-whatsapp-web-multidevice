package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type SendHandler struct {
	sendService domainSend.ISendUsecase
	resolver    deviceResolver
}

func InitMcpSend(sendService domainSend.ISendUsecase, resolver deviceResolver) *SendHandler {
	return &SendHandler{sendService: sendService, resolver: resolver}
}

func (s *SendHandler) AddSendTools(mcpServer *server.MCPServer) {
	tool := mcpg.NewTool("whatsapp_send",
		mcpg.WithDescription("Send a WhatsApp message. The `type` field selects what to send: text, image, video, audio, document, sticker, location, contact, poll, link, or forward an existing message."),
		mcpg.WithTitleAnnotation("Send WhatsApp Message"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(false),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithRawInputSchema(json.RawMessage(sendSchema)),
	)
	// NewTool defaults InputSchema.Type to "object"; clear it so only
	// RawInputSchema is set, or MarshalJSON rejects the tool as conflicting.
	tool.InputSchema = mcpg.ToolInputSchema{}
	mcpServer.AddTool(tool, s.handleSend)
	mcpServer.AddTool(s.toolSendEvent(), s.handleSendEvent)
}

func (s *SendHandler) handleSend(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, s.resolver)
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	msgType, err := request.RequireString("type")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	phone, err := request.RequireString("phone")
	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}

	base := domainSend.BaseRequest{
		ScheduleOptions: scheduleOptions(request),
		Phone:           phone,
		IsForwarded:     request.GetBool("is_forwarded", false),
	}

	var res domainSend.GenericResponse
	switch msgType {
	case "text":
		replyID := request.GetString("reply_message_id", "")
		res, err = s.sendService.SendText(ctx, domainSend.MessageRequest{
			BaseRequest:    base,
			Message:        request.GetString("message", ""),
			ReplyMessageID: &replyID,
			Mentions:       request.GetStringSlice("mentions", nil),
			AllowReshare:   request.GetBool("allow_reshare", false),
		})
	case "image":
		imageURL := request.GetString("image_url", "")
		res, err = s.sendService.SendImage(ctx, domainSend.ImageRequest{
			BaseRequest:  base,
			ImageURL:     &imageURL,
			Caption:      request.GetString("caption", ""),
			Mentions:     request.GetStringSlice("mentions", nil),
			ViewOnce:     request.GetBool("view_once", false),
			Compress:     request.GetBool("compress", true),
			HD:           request.GetBool("hd", false),
			AllowReshare: request.GetBool("allow_reshare", false),
		})
	case "video":
		videoURL := request.GetString("video_url", "")
		res, err = s.sendService.SendVideo(ctx, domainSend.VideoRequest{
			BaseRequest:  base,
			VideoURL:     &videoURL,
			Caption:      request.GetString("caption", ""),
			Mentions:     request.GetStringSlice("mentions", nil),
			ViewOnce:     request.GetBool("view_once", false),
			GifPlayback:  request.GetBool("gif_playback", false),
			Compress:     request.GetBool("compress", false),
			HD:           request.GetBool("hd", false),
			AllowReshare: request.GetBool("allow_reshare", false),
		})
	case "audio":
		audioURL := request.GetString("audio_url", "")
		res, err = s.sendService.SendAudio(ctx, domainSend.AudioRequest{
			BaseRequest: base,
			AudioURL:    &audioURL,
			PTT:         request.GetBool("ptt", false),
		})
	case "document":
		fileURL := request.GetString("file_url", "")
		res, err = s.sendService.SendFile(ctx, domainSend.FileRequest{
			BaseRequest: base,
			FileURL:     &fileURL,
			Caption:     request.GetString("caption", ""),
			Mentions:    request.GetStringSlice("mentions", nil),
		})
	case "sticker":
		stickerURL := request.GetString("sticker_url", "")
		res, err = s.sendService.SendSticker(ctx, domainSend.StickerRequest{
			BaseRequest: base,
			StickerURL:  &stickerURL,
		})
	case "location":
		res, err = s.sendService.SendLocation(ctx, domainSend.LocationRequest{
			BaseRequest: base,
			Latitude:    request.GetString("latitude", ""),
			Longitude:   request.GetString("longitude", ""),
		})
	case "contact":
		res, err = s.sendService.SendContact(ctx, domainSend.ContactRequest{
			BaseRequest:  base,
			ContactName:  request.GetString("contact_name", ""),
			ContactPhone: request.GetString("contact_phone", ""),
		})
	case "poll":
		res, err = s.sendService.SendPoll(ctx, domainSend.PollRequest{
			BaseRequest: base,
			Question:    request.GetString("question", ""),
			Options:     request.GetStringSlice("options", nil),
			MaxAnswer:   request.GetInt("max_answer", 1),
		})
	case "link":
		res, err = s.sendService.SendLink(ctx, domainSend.LinkRequest{
			BaseRequest: base,
			Link:        request.GetString("link", ""),
			Caption:     request.GetString("caption", ""),
		})
	case "forward":
		forwardReq := domainSend.ForwardRequest{
			ScheduleOptions: scheduleOptions(request),
			MessageID:       request.GetString("message_id", ""),
			Phone:           phone,
			ForceReupload:   request.GetBool("force_reupload", false),
		}
		if args := request.GetArguments(); args != nil {
			if _, ok := args["duration"]; ok {
				duration := request.GetInt("duration", 0)
				forwardReq.Duration = &duration
			}
		}
		res, err = s.sendService.SendForward(ctx, forwardReq)
	default:
		return mcpg.NewToolResultError(fmt.Sprintf("unknown send type: %s", msgType)), nil
	}

	if err != nil {
		return mcpg.NewToolResultError(err.Error()), nil
	}
	if res.ScheduleID != "" {
		return mcpg.NewToolResultStructured(res, fmt.Sprintf("%s scheduled with schedule_id %s, next run %s", msgType, res.ScheduleID, res.NextRunAt)), nil
	}
	return mcpg.NewToolResultText(fmt.Sprintf("%s sent successfully with ID %s", msgType, res.MessageID)), nil
}

func scheduleOptions(request mcpg.CallToolRequest) domainSend.ScheduleOptions {
	return domainSend.ScheduleOptions{
		ScheduledAt:     request.GetString("scheduled_at", ""),
		Timezone:        request.GetString("timezone", ""),
		Recurrence:      request.GetString("recurrence", ""),
		Weekdays:        request.GetIntSlice("weekdays", nil),
		DayOfMonth:      request.GetInt("day_of_month", 0),
		EndAt:           request.GetString("end_at", ""),
		OccurrenceLimit: request.GetInt("occurrence_limit", 0),
	}
}

func (s *SendHandler) toolSendEvent() mcpg.Tool {
	return mcpg.NewTool("whatsapp_send_event",
		mcpg.WithDescription("Send a calendar event to a WhatsApp contact or group. Recipients can RSVP to the event."),
		mcpg.WithTitleAnnotation("Send Event"),
		mcpg.WithReadOnlyHintAnnotation(false),
		mcpg.WithDestructiveHintAnnotation(false),
		mcpg.WithIdempotentHintAnnotation(false),
		mcpg.WithString("phone",
			mcpg.Required(),
			mcpg.Description("Phone number or group ID to send the event to"),
		),
		mcpg.WithString("name",
			mcpg.Required(),
			mcpg.Description("The event name"),
		),
		mcpg.WithNumber("start_time",
			mcpg.Required(),
			mcpg.Description("Event start time as a unix timestamp in seconds"),
		),
		mcpg.WithString("description",
			mcpg.Description("Optional event description"),
		),
		mcpg.WithNumber("end_time",
			mcpg.Description("Optional event end time as a unix timestamp in seconds"),
		),
		mcpg.WithString("location_name",
			mcpg.Description("Optional location name shown on the event"),
		),
		mcpg.WithBoolean("extra_guests_allowed",
			mcpg.Description("Whether attendees may bring extra guests (default: false)"),
		),
		mcpg.WithNumber("duration",
			mcpg.Description("Optional disappearing message duration in seconds (0, 86400, 604800, 7776000)"),
		),
	)
}

// int64Arg reads an MCP number argument as int64 without routing through the
// platform-sized int helpers, which truncate unix timestamps past 2038 on
// 32-bit builds (e.g. the armv7 image).
func int64Arg(request mcpg.CallToolRequest, key string) (int64, bool, error) {
	args := request.GetArguments()
	if args == nil {
		return 0, false, nil
	}
	val, exists := args[key]
	if !exists || val == nil {
		return 0, false, nil
	}
	switch v := val.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v {
			return 0, false, fmt.Errorf("argument %q must be an integer", key)
		}
		if v <= math.MinInt64 || v >= math.MaxInt64 {
			return 0, false, fmt.Errorf("argument %q is out of range", key)
		}
		return int64(v), true, nil
	case int64:
		return v, true, nil
	case int:
		return int64(v), true, nil
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("argument %q cannot be converted to int64", key)
		}
		return parsed, true, nil
	default:
		return 0, false, fmt.Errorf("argument %q is not an integer", key)
	}
}

func (s *SendHandler) handleSendEvent(ctx context.Context, request mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	ctx, _, err := resolveDeviceContext(ctx, request, s.resolver)
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	name, err := request.RequireString("name")
	if err != nil {
		return nil, err
	}

	startTime, ok, err := int64Arg(request, "start_time")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("required argument \"start_time\" not found")
	}

	eventRequest := domainSend.EventRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone: phone,
		},
		Name:               name,
		Description:        request.GetString("description", ""),
		StartTime:          startTime,
		LocationName:       request.GetString("location_name", ""),
		ExtraGuestsAllowed: request.GetBool("extra_guests_allowed", false),
	}

	// An explicitly supplied end_time is always passed through so validation
	// can reject values that are not after start_time.
	endTime, ok, err := int64Arg(request, "end_time")
	if err != nil {
		return nil, err
	}
	if ok {
		eventRequest.EndTime = &endTime
	}

	// duration goes through the same strict integer path as the timestamps:
	// RequireInt casts float64 to int, so fractional values like 86400.5
	// would silently truncate onto the duration whitelist.
	duration64, ok, err := int64Arg(request, "duration")
	if err != nil {
		return nil, err
	}
	if ok {
		if duration64 < math.MinInt32 || duration64 > math.MaxInt32 {
			return nil, errors.New("argument \"duration\" is out of range")
		}
		duration := int(duration64)
		eventRequest.Duration = &duration
	}

	res, err := s.sendService.SendEvent(ctx, eventRequest)
	if err != nil {
		return nil, err
	}

	return mcpg.NewToolResultText(fmt.Sprintf("Event sent successfully with ID %s", res.MessageID)), nil
}
