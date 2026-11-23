package web

import (
	"errors"
	"net/url"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
)

// claudeConnectorsURL opens Claude's Add custom connector dialog. Claude
// fills it from connectorName and connectorUrl (read from claude.ai's front
// end; not a documented contract, so the dialog also shows the URL itself).
const claudeConnectorsURL = "https://claude.ai/customize/connectors"

// grantView is one connected app as the Account page lists it.
type grantView struct {
	Family    string
	Host      string
	Name      string
	Connected string
	LastUsed  string
}

// connectionsData is what the Connected apps card needs, or nil when MCP is
// off (no card at all).
func connectionsData(c fiber.Ctx, d Deps, userID string) (map[string]any, error) {
	if d.Grants == nil || d.MCPURL == "" {
		return nil, nil
	}
	grants, err := d.Grants.ListGrants(c.Context(), userID)
	if err != nil {
		return nil, err
	}
	views := make([]grantView, 0, len(grants))
	for _, g := range grants {
		created, last := g.CreatedAt, g.LastUsedAt
		views = append(views, grantView{Family: g.FamilyID, Host: g.ClientHost, Name: g.ClientName, Connected: humanAgo(&created), LastUsed: humanAgo(&last)})
	}
	return map[string]any{
		"Grants":     views,
		"AddLink":    claudeAddLink(d.ProductName, d.MCPURL),
		"MCPURL":     d.MCPURL,
		"ClaudeCode": "claude mcp add --transport http " + connectorSlug(d.ProductName) + " " + d.MCPURL,
	}, nil
}

// claudeAddLink builds the one-click link with url.Values: the MCP URL is
// itself a URL, and its own ? or & must not splice into claude.ai's query.
func claudeAddLink(name, mcpURL string) string {
	v := url.Values{}
	v.Set("modal", "add-custom-connector")
	v.Set("connectorName", name)
	v.Set("connectorUrl", mcpURL)
	return claudeConnectorsURL + "?" + v.Encode()
}

// connectorSlug is a name Claude Code accepts for a server: lower-case
// letters, digits and hyphens.
func connectorSlug(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case ('a' <= r && r <= 'z') || ('0' <= r && r <= '9'):
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	if s := strings.Trim(b.String(), "-"); s != "" {
		return s
	}
	return "whatsapp"
}

// mountConnections registers Disconnect. A page route (session and CSRF
// only); it ends one of the signed-in user's own grants, and another user's
// grant is a 404 like one that does not exist.
func mountConnections(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	csrf := d.Auth.VerifyCSRF()
	app.Post("/account/connections/:family/revoke", page, csrf, func(c fiber.Ctx) error {
		if d.Grants == nil {
			return fiber.ErrNotFound
		}
		u, _ := userOf(c)
		if err := d.Grants.RevokeGrant(c.Context(), u.ID, c.Params("family")); err != nil {
			if errors.Is(err, oauth.ErrInvalidGrant) {
				return fiber.ErrNotFound
			}
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "oauth_grant_revoked", IP: c.IP()})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/account")
	})
}
