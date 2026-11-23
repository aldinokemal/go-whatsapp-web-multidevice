package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Shape: the desktop menu is either the full 248px sidebar or the 72px rail,
// never anything in between. Animating its width drew every in-between width
// (about 110px a few frames into unfolding the rail, labels wrapped onto two
// lines), and a screenshot or a slow device catches exactly that frame. So
// the width and the main column's margin snap, and a label never wraps.
func TestSidebarWidthNeverAnimates(t *testing.T) {
	css, err := staticFS.ReadFile("static/app.css")
	require.NoError(t, err)
	for _, sel := range []string{".sidebar", ".main"} {
		re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(sel) + `\s*\{([^}]*)\}`)
		for _, m := range re.FindAllStringSubmatch(string(css), -1) {
			body := m[1]
			if strings.Contains(body, "transition") {
				require.NotRegexp(t, `transition[^;]*\b(width|margin-left|all)\b`, body,
					"%s must not animate its width or margin: an in-between width squeezes the menu", sel)
			}
		}
	}
	require.Regexp(t, `(?s)\.nav-label\s*\{[^}]*white-space:\s*nowrap`, string(css), "a menu label never wraps")
}
