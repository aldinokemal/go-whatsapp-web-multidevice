package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestSigninNextAcceptsOnlySameOriginRelativePaths(t *testing.T) {
	app := fiber.New()
	app.Get("/x", func(c fiber.Ctx) error { return c.SendString(signinNext(c)) })
	for in, want := range map[string]string{
		"/account":             "/account",
		"/admin/users?x=1":     "/admin/users?x=1",
		"":                     "/",
		"//evil.example":       "/",
		"https://evil.example": "/",
		"javascript:alert(1)":  "/",
		"/\\evil.example":      "/",
		"/%2F%2Fevil.example":  "/",
		"evil.example":         "/",
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/x?next="+url.QueryEscape(in), nil))
		require.NoError(t, err)
		b, _ := io.ReadAll(resp.Body)
		require.Equal(t, want, string(b), in)
	}
}

func TestSigninCarriesNextToThePasskeyButtonAndTheVerifyPage(t *testing.T) {
	e := newEnv(t)
	resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/signin?next="+url.QueryEscape("/account"), nil))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, `data-next="/account"`, "passkey sign-in returns there")
	var next *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == nextCookie {
			next = c
		}
	}
	require.NotNil(t, next, "remembered for the WhatsApp path")
	require.Equal(t, "/account", next.Value)

	// The WhatsApp reply link opened in the same browser lands on /account.
	_, _ = e.st.CreateUser(context.Background(), "+27825550101")
	raw, _ := e.st.CreateLoginToken(context.Background(), "+27825550101", 15*time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/signin/verify?t="+url.QueryEscape(raw), nil)
	req.AddCookie(next)
	_, body = e.do(t, req)
	require.Contains(t, body, `name="next" value="/account"`)

	form := url.Values{"t": {raw}, "next": {"/account"}}
	post := httptest.NewRequest(http.MethodPost, "/signin/verify", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ = e.do(t, post)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/account", resp.Header.Get("Location"))
}

func TestSigninRefusesAnOpenRedirectInNext(t *testing.T) {
	e := newEnv(t)
	for _, bad := range []string{"//evil.example", "https://evil.example/x", "/%2F%2Fevil.example"} {
		resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/signin?next="+url.QueryEscape(bad), nil))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Contains(t, body, `data-next="/"`, bad)
		for _, c := range resp.Cookies() {
			require.NotEqual(t, nextCookie, c.Name, "nothing unsafe is remembered: %s", bad)
		}
	}
	// A forged cookie is validated again on the verify page.
	_, _ = e.st.CreateUser(context.Background(), "+27825550101")
	raw, _ := e.st.CreateLoginToken(context.Background(), "+27825550101", 15*time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/signin/verify?t="+url.QueryEscape(raw), nil)
	req.AddCookie(&http.Cookie{Name: nextCookie, Value: "//evil.example"})
	_, body := e.do(t, req)
	require.Contains(t, body, `name="next" value="/"`)
}

func TestDisabledUserOpeningASignInLinkIsToldTheAccountIsDisabled(t *testing.T) {
	e := newEnv(t)
	u, _ := e.st.CreateUser(context.Background(), "+27825550101")
	raw, _ := e.st.CreateLoginToken(context.Background(), "+27825550101", 15*time.Minute)
	require.NoError(t, e.st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled))

	_, body := e.do(t, httptest.NewRequest(http.MethodGet, "/signin/verify?t="+url.QueryEscape(raw), nil))
	require.Contains(t, body, "This account is disabled. Contact your admin.")
	require.NotContains(t, body, "Link expired")
	require.NotContains(t, body, `method="post"`, "nothing to confirm")

	form := url.Values{"t": {raw}}
	post := httptest.NewRequest(http.MethodPost, "/signin/verify", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, body := e.do(t, post)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "This account is disabled. Contact your admin.")
}
