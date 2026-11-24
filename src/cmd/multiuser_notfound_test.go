package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Security and behaviour: a browser asking for a page that does not exist
// gets the styled 404 page, signed in or not, and never a JSON error or the
// API gate's 401. A machine path (and a client that does not accept HTML)
// still gets JSON.
func TestAnUnknownPageIsTheStyledNotFound(t *testing.T) {
	app, rt, _ := newMultiUserApp(t, uimcp.Deps{})
	ctx := context.Background()
	alice, err := rt.Store.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	raw, err := rt.Store.CreateSession(ctx, alice.ID, "", time.Hour)
	require.NoError(t, err)
	cookie := &http.Cookie{Name: rt.Auth.CookieName(), Value: raw}

	get := func(path, accept string, signedIn bool) (int, string, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		if signedIn {
			req.AddCookie(cookie)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
	}
	const browser = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	for _, signedIn := range []bool{true, false} {
		for _, path := range []string{"/nope", "/admin/nope", "/account/x/y"} {
			code, ctype, body := get(path, browser, signedIn)
			assert.Equal(t, http.StatusNotFound, code, "signed in=%v %s", signedIn, path)
			assert.Contains(t, ctype, "text/html", "signed in=%v %s", signedIn, path)
			assert.Contains(t, body, "Error 404", "signed in=%v %s", signedIn, path)
			assert.NotContains(t, body, "VALIDATION_ERROR")
		}

		// A machine path keeps answering in JSON, whatever the client accepts.
		code, ctype, _ := get("/user/nope", browser, signedIn)
		assert.NotEqual(t, http.StatusOK, code)
		assert.Contains(t, ctype, "application/json", "signed in=%v machine path", signedIn)

		// A client that wants JSON is not handed a page.
		_, ctype, _ = get("/nope", "application/json", signedIn)
		assert.Contains(t, ctype, "application/json", "signed in=%v JSON client", signedIn)
	}

	// A real route registered behind the API gate, outside the machine
	// prefixes, is not swallowed: it stays gated.
	app.Get("/plain/:id/thing", func(c fiber.Ctx) error { return c.SendString("ok") })
	code, _, _ := get("/plain/7/thing", browser, false)
	assert.Equal(t, http.StatusUnauthorized, code, "a real route is gated, not a 404")
	code, _, body := get("/plain/7/thing", browser, true)
	assert.NotEqual(t, http.StatusNotFound, code, body)
	assert.NotContains(t, body, "Error 404")
	// Real pages still render.
	code, _, _ = get("/signin", browser, false)
	assert.Equal(t, http.StatusOK, code)
}
