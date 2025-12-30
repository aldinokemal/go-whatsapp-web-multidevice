package middleware

import (
	"context"
	"fmt"

	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

func Recovery() fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		defer func() {
			err := recover()
			if err != nil {
				var res utils.ResponseData
				res.Status = 500
				res.Code = "INTERNAL_SERVER_ERROR"
				res.Message = fmt.Sprintf("%v", err)

				// Log the panic using logrus
				logrus.Errorf("Panic recovered in middleware: %v", err)

				// Check for context deadline exceeded (timeout)
				if ctxErr, ok := err.(error); ok && ctxErr == context.DeadlineExceeded {
					res.Status = 504
					res.Code = "GATEWAY_TIMEOUT"
					res.Message = "Request timed out waiting for WhatsApp server response"
				}

				errValidation, isValidationError := err.(pkgError.GenericError)
				if isValidationError {
					res.Status = errValidation.StatusCode()
					res.Code = errValidation.ErrCode()
					res.Message = errValidation.Error()
				}

				_ = ctx.Status(res.Status).JSON(res)
			}
		}()

		return ctx.Next()
	}
}
