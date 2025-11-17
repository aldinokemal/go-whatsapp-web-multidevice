package rest

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Session struct{}

func InitRestSession(app fiber.Router) Session {
	rest := Session{}
	app.Get("/sessions", rest.ListSessions)
	app.Get("/sessions/:id/status", rest.GetSessionStatus)
	app.Post("/sessions/:id/set-default", rest.SetDefaultSession)

	return Session{}
}

// ListSessions returns all available WhatsApp sessions with their status
func (handler *Session) ListSessions(c *fiber.Ctx) error {
	sm := whatsapp.GetSessionManager()
	sessions := sm.GetAllSessionsWithStatus()

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Sessions retrieved successfully",
		Results: map[string]any{
			"sessions": sessions,
			"count":    len(sessions),
		},
	})
}

// GetSessionStatus returns the connection status for a specific session
func (handler *Session) GetSessionStatus(c *fiber.Ctx) error {
	sessionID := c.Params("id")
	if sessionID == "" {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "ERROR",
			Message: "Session ID is required",
		})
	}

	sm := whatsapp.GetSessionManager()
	isConnected, isLoggedIn, deviceID, err := sm.GetConnectionStatus(sessionID)
	if err != nil {
		return c.Status(404).JSON(utils.ResponseData{
			Status:  404,
			Code:    "ERROR",
			Message: err.Error(),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Session status retrieved",
		Results: map[string]any{
			"session_id":   sessionID,
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
			"device_id":    deviceID,
		},
	})
}

// SetDefaultSession sets a session as the default session
func (handler *Session) SetDefaultSession(c *fiber.Ctx) error {
	sessionID := c.Params("id")
	if sessionID == "" {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "ERROR",
			Message: "Session ID is required",
		})
	}

	sm := whatsapp.GetSessionManager()
	if err := sm.SetDefaultSession(sessionID); err != nil {
		return c.Status(404).JSON(utils.ResponseData{
			Status:  404,
			Code:    "ERROR",
			Message: err.Error(),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Default session updated",
		Results: map[string]any{
			"session_id": sessionID,
		},
	})
}
