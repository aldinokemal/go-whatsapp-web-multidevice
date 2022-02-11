package middleware

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
)

func Recovery() fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		defer func() {
			err := recover()
			if err != nil {
				var res utils.ResponseData
				res.Code = 500
				res.Message = fmt.Sprintf("%s", err)

				errValidation, okValidation := err.(utils.ValidationError)
				if okValidation {
					res.Code = 400
					res.Message = errValidation.Message
				}

				errAuth, okAuth := err.(utils.AuthError)
				if okAuth {
					res.Code = 401
					res.Message = errAuth.Message
				}

				_ = ctx.Status(res.Code).JSON(res)
			}
		}()

		return ctx.Next()
	}
}
