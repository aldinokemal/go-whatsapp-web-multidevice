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

				dt, ok := err.(utils.ValidationError)
				if ok {
					res.Code = 400
					res.Message = dt.Message
				} else {
					res.Code = 500
					res.Message = fmt.Sprintf("%s", err)
				}

				_ = ctx.Status(res.Code).JSON(res)
			}
		}()

		return ctx.Next()
	}
}
