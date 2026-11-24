package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shape (house rules B2, B3): a kebab trigger is the icon alone with an
// accessible name, and in a row it sits before (left of) the row's button.
func TestKebabMenusFollowTheHouseRules(t *testing.T) {
	src, err := templatesFS.ReadFile("templates/components.html")
	require.NoError(t, err)
	trigger := regexp.MustCompile(`<button[^>]*data-menu-trigger[^>]*>(.*?)</button>`).FindStringSubmatch(string(src))
	require.NotNil(t, trigger)
	require.Contains(t, trigger[0], `aria-label="{{.}}"`)
	require.Equal(t, `{{icon "ellipsis"}}`, trigger[1], "the trigger never says a word")

	users, err := templatesFS.ReadFile("templates/admin_users.html")
	require.NoError(t, err)
	body := string(users)
	menu := strings.Index(body, `{{template "menu_start"`)
	primary := strings.Index(body, `Get sign-in link</span>`)
	require.True(t, menu > 0 && primary > menu, "the kebab comes before the row's primary button")
}
