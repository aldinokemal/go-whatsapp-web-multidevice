package cmd

import (
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/basicauth"
	"github.com/gofiber/fiber/v3/middleware/proxy"
	"github.com/sirupsen/logrus"
)

const csAIProxyTimeout = 60 * time.Second

// registerCSAIProxy mounts {AppBasePath}/cs-ai and forwards it to the
// hermes-cs-bridge settings page. It must be mounted after the Basic Auth
// middleware: the bridge trusts X-CS-AI-Token as proof that GOWA already
// authenticated the user, so the route is refused when Basic Auth is off.
func registerCSAIProxy(r fiber.Router) {
	if config.CSAIAdminURL == "" || config.CSAIAdminToken == "" {
		return
	}
	if len(config.AppBasicAuthCredential) == 0 {
		logrus.Warn("[CS_AI] CS_AI_ADMIN_URL is set but APP_BASIC_AUTH is empty; /cs-ai is NOT mounted (it would be public)")
		return
	}
	handler := newCSAIProxyHandler(strings.TrimRight(config.CSAIAdminURL, "/"), config.CSAIAdminToken, config.AppBasePath)
	r.All("/cs-ai", handler)
	r.All("/cs-ai/*", handler)
	logrus.Infof("[CS_AI] settings page mounted at %s/cs-ai", config.AppBasePath)
}

func newCSAIProxyHandler(target, token, basePath string) fiber.Handler {
	return func(c fiber.Ctx) error {
		// OriginalURL keeps the raw path and query; the bridge serves /cs-ai/...
		upstream := target + strings.TrimPrefix(c.OriginalURL(), basePath)

		h := &c.Request().Header
		h.Del(fiber.HeaderAuthorization) // never forward the GOWA password
		h.Del(fiber.HeaderCookie)
		h.Set("X-CS-AI-Token", token)
		h.Set("X-CS-AI-User", basicauth.UsernameFromContext(c))

		if err := proxy.DoTimeout(c, upstream, csAIProxyTimeout); err != nil {
			logrus.Warnf("[CS_AI] bridge unreachable at %s: %v", target, err)
			return c.Status(fiber.StatusBadGateway).SendString("CS AI bridge tidak terjangkau. Cek: docker compose ps hermes_cs_bridge")
		}
		c.Response().Header.Del("X-CS-AI-Token")
		return nil
	}
}
