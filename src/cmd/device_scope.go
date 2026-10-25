package cmd

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/basicauth"
)

// Device-scoped Basic Auth accounts (APP_BASIC_AUTH_DEVICES).
//
// Basic Auth alone lets every account use every device: X-Device-Id is a
// selector, not a permission. An account listed in APP_BASIC_AUTH_DEVICES is
// "restricted": it may only use the listed devices, and only for messaging
// endpoints. It cannot manage devices (login, logout, delete, webhooks), reach
// MCP, the websocket, the dashboard UI or /cs-ai. Accounts not listed keep full
// access, so existing setups are unaffected.

// parseBasicAuthDevices parses "user:dev1|dev2,user2:dev3".
func parseBasicAuthDevices(raw string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		user, devs, ok := strings.Cut(entry, ":")
		user = strings.TrimSpace(user)
		if !ok || user == "" {
			return nil, fmt.Errorf("APP_BASIC_AUTH_DEVICES entry %q: use <user>:<device>|<device>", entry)
		}
		if _, dup := out[user]; dup {
			return nil, fmt.Errorf("APP_BASIC_AUTH_DEVICES: user %q listed twice", user)
		}
		var list []string
		for _, d := range strings.Split(devs, "|") {
			if d = strings.TrimSpace(d); d != "" {
				list = append(list, d)
			}
		}
		if len(list) == 0 {
			return nil, fmt.Errorf("APP_BASIC_AUTH_DEVICES: user %q has no devices", user)
		}
		out[user] = list
	}
	return out, nil
}

// validateBasicAuthDevices checks that every restricted user exists in APP_BASIC_AUTH.
func validateBasicAuthDevices(scopes map[string][]string, credentials []string) error {
	if len(scopes) == 0 {
		return nil
	}
	if len(credentials) == 0 {
		return fmt.Errorf("APP_BASIC_AUTH_DEVICES is set but APP_BASIC_AUTH is empty")
	}
	known := map[string]bool{}
	for _, c := range credentials {
		user, _, _ := strings.Cut(c, ":")
		known[user] = true
	}
	var missing []string
	for user := range scopes {
		if !known[user] {
			missing = append(missing, user)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("APP_BASIC_AUTH_DEVICES names users missing from APP_BASIC_AUTH: %s", strings.Join(missing, ", "))
	}
	return nil
}

// isRestrictedAccount reports whether username is device-scoped.
func isRestrictedAccount(username string) bool {
	_, ok := config.AppBasicAuthDevices[username]
	return ok
}

// deviceResolver maps a requested device id (alias or JID) to its canonical id.
type deviceResolver func(id string) (canonical string, found bool)

// scopedRoute reports whether a restricted account may call method+p at all,
// and whether the target device comes from the path (/devices/:id/...).
func scopedRoute(method, p string) (allowed bool, pathDevice string) {
	isGet := method == fiber.MethodGet || method == fiber.MethodHead
	seg := strings.Split(strings.TrimPrefix(p, "/"), "/")
	switch {
	case strings.HasPrefix(p, "/send/"), strings.HasPrefix(p, "/message/"), strings.HasPrefix(p, "/chat/"),
		p == "/group", strings.HasPrefix(p, "/group/"), strings.HasPrefix(p, "/newsletter/"), strings.HasPrefix(p, "/call/"):
		return true, ""
	case isGet && (p == "/chats" || strings.HasPrefix(p, "/user/") || p == "/url" || p == "/app/info"):
		return true, ""
	case isGet && len(seg) >= 2 && len(seg) <= 3 && seg[0] == "devices" && seg[1] != "" &&
		(len(seg) == 2 || seg[2] == "status"):
		// GET /devices/:id and GET /devices/:id/status for the account's own device.
		return true, seg[1]
	}
	return false, ""
}

func scopeError(c fiber.Ctx, status int, code, msg string) error {
	return c.Status(status).JSON(utils.ResponseData{Status: status, Code: code, Message: msg})
}

// newDeviceScopeMiddleware enforces APP_BASIC_AUTH_DEVICES. It runs after the
// Basic Auth middleware, which has already verified the password.
func newDeviceScopeMiddleware(scopes map[string][]string, basePath string, resolve deviceResolver) fiber.Handler {
	allowedSet := make(map[string]map[string]bool, len(scopes))
	for user, devs := range scopes {
		set := map[string]bool{}
		for _, d := range devs {
			set[d] = true
		}
		allowedSet[user] = set
	}
	permitted := func(user, requested string) bool {
		set := allowedSet[user]
		if set[requested] {
			return true
		}
		if canonical, ok := resolve(requested); ok && set[canonical] {
			return true
		}
		// The allowed entry may be a JID or alias of the same device.
		for d := range set {
			if c, ok := resolve(d); ok {
				if rc, ok2 := resolve(requested); ok2 && rc == c {
					return true
				}
			}
		}
		return false
	}

	return func(c fiber.Ctx) error {
		user := basicauth.UsernameFromContext(c)
		devices, restricted := scopes[user]
		if !restricted {
			return c.Next()
		}

		// Fiber routing is case-insensitive and tolerant of "//" and "/./",
		// so normalise the same way before matching.
		p := strings.ToLower(path.Clean("/" + c.Path()))
		if bp := strings.ToLower(strings.TrimRight(basePath, "/")); bp != "" {
			if p != bp && !strings.HasPrefix(p, bp+"/") {
				return scopeError(c, fiber.StatusForbidden, "FORBIDDEN_FOR_ACCOUNT", "this account is limited to messaging endpoints")
			}
			p = strings.TrimPrefix(p, bp)
			if p == "" {
				p = "/"
			}
		}

		ok, pathDevice := scopedRoute(c.Method(), p)
		if !ok {
			return scopeError(c, fiber.StatusForbidden, "FORBIDDEN_FOR_ACCOUNT",
				"this account is limited to messaging endpoints of its own devices")
		}
		if pathDevice != "" {
			// Take the id from the original path to keep its casing and encoding.
			orig := strings.Split(strings.TrimPrefix(c.Path(), "/"), "/")
			for i, s := range orig {
				if strings.EqualFold(s, "devices") && i+1 < len(orig) {
					pathDevice = orig[i+1]
					break
				}
			}
			if d, err := url.PathUnescape(pathDevice); err == nil {
				pathDevice = d
			}
			if !permitted(user, pathDevice) {
				return scopeError(c, fiber.StatusForbidden, "DEVICE_NOT_ALLOWED", "this account may not use device "+pathDevice)
			}
			return c.Next()
		}

		requested := strings.TrimSpace(c.Get(middleware.DeviceIDHeader))
		if decoded, err := url.QueryUnescape(requested); err == nil {
			requested = decoded
		}
		if requested == "" {
			requested = strings.TrimSpace(c.Query("device_id"))
		}
		if requested == "" {
			if len(devices) != 1 {
				return scopeError(c, fiber.StatusBadRequest, "DEVICE_ID_REQUIRED",
					"this account has several devices; send X-Device-Id")
			}
			// Never fall back to the server's default device: pin the only allowed one.
			c.Request().Header.Set(middleware.DeviceIDHeader, devices[0])
			return c.Next()
		}
		if !permitted(user, requested) {
			return scopeError(c, fiber.StatusForbidden, "DEVICE_NOT_ALLOWED", "this account may not use device "+requested)
		}
		return c.Next()
	}
}
