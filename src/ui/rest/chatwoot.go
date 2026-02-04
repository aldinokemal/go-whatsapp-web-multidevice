package rest

import (
	"fmt"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

type ChatwootHandler struct {
	AppUsecase    domainApp.IAppUsecase
	SendUsecase   domainSend.ISendUsecase
	DeviceManager *whatsapp.DeviceManager
}

func NewChatwootHandler(appUsecase domainApp.IAppUsecase, sendUsecase domainSend.ISendUsecase, dm *whatsapp.DeviceManager) *ChatwootHandler {
	return &ChatwootHandler{
		AppUsecase:    appUsecase,
		SendUsecase:   sendUsecase,
		DeviceManager: dm,
	}
}

func (h *ChatwootHandler) HandleWebhook(c *fiber.Ctx) error {
	logrus.Debugf("Chatwoot Webhook raw body: %s", string(c.Body()))

	// Resolve device for outbound messages
	instance, resolvedID, err := h.DeviceManager.ResolveDevice(config.ChatwootDeviceID)
	if err != nil {
		logrus.Errorf("Chatwoot Webhook: Failed to resolve device: %v", err)
		return c.Status(fiber.StatusServiceUnavailable).JSON(utils.ResponseData{
			Status:  fiber.StatusServiceUnavailable,
			Code:    "DEVICE_NOT_AVAILABLE",
			Message: fmt.Sprintf("No device available for Chatwoot: %v. Configure CHATWOOT_DEVICE_ID or ensure one device is registered.", err),
		})
	}
	logrus.Debugf("Chatwoot Webhook: Using device %s", resolvedID)

	// Set device context for send operations
	c.SetUserContext(whatsapp.ContextWithDevice(c.UserContext(), instance))

	var payload chatwoot.WebhookPayload
	if err := c.BodyParser(&payload); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}

	contact := payload.Conversation.Meta.Sender
	logrus.Debugf("Chatwoot Webhook: event=%s message_type=%s contact_id=%d contact_phone=%s",
		payload.Event, payload.MessageType, contact.ID, contact.PhoneNumber)

	if payload.Event != "message_created" {
		return c.SendStatus(fiber.StatusOK)
	}

	if payload.MessageType != "outgoing" {
		return c.SendStatus(fiber.StatusOK)
	}

	if payload.Private {
		return c.SendStatus(fiber.StatusOK)
	}

	customAttrs := contact.CustomAttributes
	var destination string
	if val, ok := customAttrs["waha_whatsapp_jid"]; ok {
		if strVal, ok := val.(string); ok {
			destination = strVal
		}
	}
	if destination == "" && contact.PhoneNumber != "" {
		destination = contact.PhoneNumber
	}

	if destination == "" {
		logrus.Warnf("Chatwoot Webhook: No destination phone for contact ID %d", contact.ID)
		return c.SendStatus(fiber.StatusOK)
	}

	// Check if this is a group message (JID ends with @g.us)
	isGroup := utils.IsGroupJID(destination)

	// Clean up the destination for WhatsApp sending
	destination = utils.CleanPhoneForWhatsApp(destination)

	// For private chats, strip the @s.whatsapp.net suffix if present
	// For groups, keep the full JID (including @g.us)
	if !isGroup {
		destination = utils.ExtractPhoneFromJID(destination)
	}

	logrus.Debugf("Chatwoot Webhook: Sending to destination=%s isGroup=%v", destination, isGroup)

	// Handle attachments if present
	if len(payload.Attachments) > 0 {
		for _, attachment := range payload.Attachments {
			if err := h.handleAttachment(c, destination, attachment, payload.Content); err != nil {
				logrus.Errorf("Chatwoot Webhook: Failed to send attachment %d: %v", attachment.ID, err)
			}
		}
		// Return early after sending attachments - caption was already included
		return c.SendStatus(fiber.StatusOK)
	}

	// If content is present (and not just an attachment caption), send it as text
	if payload.Content != "" {
		req := domainSend.MessageRequest{
			Message: payload.Content,
		}
		req.Phone = destination

		_, err := h.SendUsecase.SendText(c.Context(), req)
		if err != nil {
			// Log with more context but still return 200 to prevent Chatwoot retries
			logrus.WithFields(logrus.Fields{
				"destination": destination,
				"is_group":    isGroup,
				"error":       err.Error(),
			}).Error("Chatwoot Webhook: Failed to send message (returning 200 to prevent retry)")
			return c.SendStatus(fiber.StatusOK)
		}
		logrus.Infof("Chatwoot Webhook: Sent text message to %s", destination)
	}

	return c.SendStatus(fiber.StatusOK)
}

func (h *ChatwootHandler) handleAttachment(c *fiber.Ctx, phone string, att chatwoot.Attachment, caption string) error {
	switch att.FileType {
	case "image":
		req := domainSend.ImageRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			Caption:     caption,
			ImageURL:    &att.DataURL,
		}
		_, err := h.SendUsecase.SendImage(c.Context(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent image attachment to %s", phone)
		}
		return err

	case "audio":
		req := domainSend.AudioRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			AudioURL:    &att.DataURL,
			PTT:         true, // Send as PTT (Voice Note) for better mobile experience
		}
		_, err := h.SendUsecase.SendAudio(c.Context(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent audio attachment to %s", phone)
			return nil
		}

		logrus.Warnf("Chatwoot Webhook: Failed to send as audio (%v), retrying as file...", err)
		// Fallback to sending as file
		reqFile := domainSend.FileRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			FileURL:     &att.DataURL,
			Caption:     caption,
		}
		_, err = h.SendUsecase.SendFile(c.Context(), reqFile)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent audio as file attachment to %s", phone)
		}
		return err

	case "video":
		req := domainSend.VideoRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			Caption:     caption,
			VideoURL:    &att.DataURL,
		}
		_, err := h.SendUsecase.SendVideo(c.Context(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent video attachment to %s", phone)
		}
		return err

	default:
		// Default to file for other types
		req := domainSend.FileRequest{
			BaseRequest: domainSend.BaseRequest{Phone: phone},
			FileURL:     &att.DataURL,
			Caption:     caption,
		}
		_, err := h.SendUsecase.SendFile(c.Context(), req)
		if err == nil {
			logrus.Infof("Chatwoot Webhook: Sent file attachment to %s", phone)
		}
		return err
	}
}
