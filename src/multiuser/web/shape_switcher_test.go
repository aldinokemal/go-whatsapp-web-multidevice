package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shape (house rules B4, S10): every switcher that navigates is the one
// nav_switcher partial, and on a phone it takes the column's full width, so
// Users and Waitlist draw it alike.
func TestEverySwitcherIsTheOnePartial(t *testing.T) {
	entries, err := templatesFS.ReadDir("templates")
	require.NoError(t, err)
	def := regexp.MustCompile(`(?s)\{\{define "nav_switcher"\}\}.*?\{\{end\}\}\s*\n\n`)
	for _, f := range entries {
		b, _ := templatesFS.ReadFile("templates/" + f.Name())
		src := def.ReplaceAllString(string(b), "")
		require.NotContains(t, src, "data-nav-select", "%s: a navigating select outside nav_switcher", f.Name())
		require.NotContains(t, src, `class="switcher switcher-nav"`, "%s: a hand-made nav switcher", f.Name())
	}
	css, err := staticFS.ReadFile("static/app.css")
	require.NoError(t, err)
	phone := string(css)[strings.LastIndex(string(css), "@media (max-width: 600px) {\n  .list-tools"):]
	require.Contains(t, phone, ".switcher-nav { align-self: stretch; width: 100%; }")
}
