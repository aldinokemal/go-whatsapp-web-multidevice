package middleware

import (
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
