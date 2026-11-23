package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/stretchr/testify/require"
)

func renameEnv(t *testing.T) (*env, *http.Cookie, string, string) {
	t.Helper()
	stub := &devStub{devices: []domainDevice.Device{
		{ID: "d_mine", DisplayName: "Acme Support", PhoneNumber: "27825550142", State: domainDevice.DeviceStateLoggedIn, CreatedAt: time.Now().Add(-48 * time.Hour)},
		{ID: "d_theirs", PhoneNumber: "27825550177", State: domainDevice.DeviceStateLoggedIn},
	}}
	e := withLabelRoutes(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_theirs", them))
	return e, ck, me, csrfFor(t, e, ck)
}

func htmxLabel(t *testing.T, e *env, ck *http.Cookie, csrf, id string, form url.Values) (*http.Response, string) {
	t.Helper()
	form.Set("csrf", csrf)
	req := httptest.NewRequest(http.MethodPost, "/ui/devices/"+id+"/label", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(ck)
	return e.do(t, req)
}

// Behaviour: Rename opens a form holding the current name.
func TestRenameDialogHoldsTheCurrentName(t *testing.T) {
	e, ck, me, _ := renameEnv(t)
	require.NoError(t, e.st.SetLabel(context.Background(), me, "d_mine", "Work phone"))
	body := get(t, e, ck, "/ui/devices/d_mine/label")
	require.Contains(t, body, `name="label"`)
	require.Contains(t, body, `value="Work phone"`)
	require.Contains(t, body, "Remove name", "a set name can be cleared")
	require.Contains(t, get(t, e, ck, "/"), `hx-get="/ui/devices/d_mine/label"`, "each number offers Rename")

	req := httptest.NewRequest(http.MethodGet, "/ui/devices/d_theirs/label", nil)
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "another user's number")
}

// Behaviour: saving swaps the row in place (the name as its title, the
// number beneath) and closes the dialog.
func TestRenameSwapsTheRow(t *testing.T) {
	e, ck, _, csrf := renameEnv(t)
	resp, body := htmxLabel(t, e, ck, csrf, "d_mine", url.Values{"label": {"Shop till"}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, `data-close-dialog-now`)
	row := regexp.MustCompile(`(?s)<li class="row" id="row-d_mine"[^>]*hx-swap-oob="true".*?</li>`).FindString(body)
	require.NotEmpty(t, row, "the row comes back out of band")
	require.Regexp(t, `class="row-title">Shop till`, row)
	require.Contains(t, row, "&#43;27 82 555 0142", "the number moves beneath")
}

// Behaviour: the row title is the name, then the WhatsApp name, then the
// number; removing the name restores the earlier title.
func TestRowShowsLabelThenNumber(t *testing.T) {
	e, ck, me, csrf := renameEnv(t)
	home := get(t, e, ck, "/")
	require.Regexp(t, `class="row-title">Acme Support`, home)

	require.NoError(t, e.st.SetLabel(context.Background(), me, "d_mine", "Front desk"))
	home = get(t, e, ck, "/")
	require.Regexp(t, `class="row-title">Front desk`, home)
	require.Contains(t, home, "&#43;27 82 555 0142")

	resp, body := htmxLabel(t, e, ck, csrf, "d_mine", url.Values{"label": {"Front desk"}, "clear": {"1"}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Regexp(t, `class="row-title">Acme Support`, body)
	labels, _ := e.st.LabelsOf(context.Background(), me)
	require.Equal(t, "", labels["d_mine"])
}

// Behaviour: a refused name comes back in the dialog with what was typed,
// not as a bare 422.
func TestRenameErrorsStayInTheDialog(t *testing.T) {
	e, ck, me, csrf := renameEnv(t)
	long := strings.Repeat("a", 41)
	resp, body := htmxLabel(t, e, ck, csrf, "d_mine", url.Values{"label": {long}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "40 characters or fewer")
	require.Contains(t, body, `class="form-error"`)
	require.Contains(t, body, `value="`+long+`"`, "what was typed is kept")
	require.Contains(t, body, `aria-invalid="true"`)
	require.NotContains(t, body, "data-close-dialog-now")
	labels, _ := e.st.LabelsOf(context.Background(), me)
	require.Equal(t, "", labels["d_mine"])
}

// Security: a name is text wherever it is drawn: the row on My WhatsApp,
// the rename form and the swapped row all escape it.
func TestLabelIsEscapedInHTML(t *testing.T) {
	e, ck, me, csrf := renameEnv(t)
	evil := `<script>alert(1)</script>`
	require.NoError(t, e.st.SetLabel(context.Background(), me, "d_mine", evil))
	for _, body := range []string{
		get(t, e, ck, "/"),
		get(t, e, ck, "/ui/devices/d_mine/label"),
		func() string {
			_, b := htmxLabel(t, e, ck, csrf, "d_mine", url.Values{"label": {"<b>x</b>"}})
			return b
		}(),
	} {
		require.NotContains(t, body, "<script>alert(1)")
		require.NotContains(t, body, "<b>x</b>")
	}
	require.Contains(t, get(t, e, ck, "/"), "&lt;b&gt;x&lt;/b&gt;")
}

// Shape (house rule B1): a number's row shows at most three controls; the
// kebab counts as one and what it holds does not.
func TestDeviceRowHasAtMostThreeControls(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{
		{ID: "d_on", PhoneNumber: "27825550142", State: domainDevice.DeviceStateLoggedIn},
		{ID: "d_off", PhoneNumber: "27825550143", State: domainDevice.DeviceStateDisconnected},
		{ID: "d_slot"},
	}}
	e := withLabelRoutes(t, stub)
	ck, me := signedIn(t, e, "+27825550100") // a superadmin: the most actions
	for _, id := range []string{"d_on", "d_off", "d_slot"} {
		require.NoError(t, e.st.SetOwner(context.Background(), id, me))
	}
	require.NoError(t, e.st.SetMaxDevices(context.Background(), me, 5))
	home := get(t, e, ck, "/")
	rows := regexp.MustCompile(`(?s)<div class="row-actions">(.*?)</div>\s*</li>`).FindAllStringSubmatch(home, -1)
	require.Len(t, rows, 3)
	menu := regexp.MustCompile(`(?s)<div class="menu-list".*?</div>`)
	ctl := regexp.MustCompile(`<button|<a class="btn`)
	for _, r := range rows {
		outside := menu.ReplaceAllString(r[1], "")
		require.LessOrEqual(t, len(ctl.FindAllString(outside, -1)), 3, outside)
	}
}
