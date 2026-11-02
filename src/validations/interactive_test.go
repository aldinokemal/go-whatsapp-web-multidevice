package validations

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/stretchr/testify/require"
)

func TestValidateInteractive(t *testing.T) {
	for _, tc := range []struct {
		name, button, params string
		valid                bool
	}{
		{"reply", "quick_reply", `{"display_text":"Yes","id":"yes"}`, true},
		{"selection title", "single_select", `{"title":"Choose","sections":[{"title":"Options","rows":[{"id":"a","title":"A"}]}]}`, true},
		{"selection missing title", "single_select", `{"display_text":"Choose","sections":[{}]}`, false},
		{"null section", "single_select", `{"title":"Choose","sections":[null]}`, false},
		{"missing rows", "single_select", `{"title":"Choose","sections":[{}]}`, false},
		{"empty rows", "single_select", `{"title":"Choose","sections":[{"rows":[]}]}`, false},
		{"null row", "single_select", `{"title":"Choose","sections":[{"rows":[null]}]}`, false},
		{"blank row id", "single_select", `{"title":"Choose","sections":[{"rows":[{"id":" ","title":"A"}]}]}`, false},
		{"nonstring row title", "single_select", `{"title":"Choose","sections":[{"rows":[{"id":"a","title":1}]}]}`, false},
		{"location empty params", "send_location", `{}`, true},
		{"buy", "cta_url", `{"display_text":"Buy now","url":"https://example.com"}`, true},
		{"location", "send_location", `{"display_text":"Share location"}`, true},
		{"invalid url", "cta_url", `{"display_text":"Buy","url":"javascript:alert(1)"}`, false},
		{"missing id", "quick_reply", `{"display_text":"Yes"}`, false},
		{"unknown", "payment", `{}`, false},
		{"array", "quick_reply", `[]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := domainSend.InteractiveRequest{Phone: "15551234567", Body: "Choose", Buttons: []domainSend.NativeFlowButton{{Name: tc.button, Params: json.RawMessage(tc.params)}}}
			err := ValidateInteractive(context.Background(), req)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	b := domainSend.NativeFlowButton{Name: "quick_reply", Params: json.RawMessage(`{"display_text":"Yes","id":"yes"}`)}
	req := domainSend.InteractiveRequest{Phone: "15551234567", Body: "Cards", Cards: []domainSend.InteractiveCard{{Body: "One", Buttons: []domainSend.NativeFlowButton{b}}, {Body: "Two", Buttons: []domainSend.NativeFlowButton{b}}}}
	require.NoError(t, ValidateInteractive(context.Background(), req))
	req.Buttons = []domainSend.NativeFlowButton{b}
	require.Error(t, ValidateInteractive(context.Background(), req))
	req.Buttons = nil
	req.Cards = req.Cards[:1]
	require.Error(t, ValidateInteractive(context.Background(), req))
}

// TestInteractiveRequiredFieldsAndImageLimit covers struct rules and the shared media limit.
func TestInteractiveRequiredFieldsAndImageLimit(t *testing.T) {
	b := domainSend.NativeFlowButton{Name: "quick_reply", Params: json.RawMessage(`{"display_text":"Yes","id":"yes"}`)}
	req := domainSend.InteractiveRequest{Phone: "15551234567", Body: "Choose", Buttons: []domainSend.NativeFlowButton{b}}
	missing := req
	missing.Phone = ""
	require.Error(t, ValidateInteractive(context.Background(), missing))
	missing = req
	missing.Body = ""
	require.Error(t, ValidateInteractive(context.Background(), missing))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	req.Buttons = nil
	req.Cards = []domainSend.InteractiveCard{{Body: "One", ImageBase64: base64.StdEncoding.EncodeToString(buf.Bytes()), Buttons: []domainSend.NativeFlowButton{b}}, {Body: "Two", Buttons: []domainSend.NativeFlowButton{b}}}
	previous := config.WhatsappSettingMaxImageSize
	t.Cleanup(func() { config.WhatsappSettingMaxImageSize = previous })
	config.WhatsappSettingMaxImageSize = int64(buf.Len())
	require.NoError(t, ValidateInteractive(context.Background(), req))
	config.WhatsappSettingMaxImageSize--
	require.ErrorContains(t, ValidateInteractive(context.Background(), req), "configured image limit")
}
