package validations

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// ValidateInteractive checks required fields, native-flow choices, and carousel media before sending.
func ValidateInteractive(ctx context.Context, req domainSend.InteractiveRequest) error {
	if err := validation.ValidateStructWithContext(ctx, &req,
		validation.Field(&req.Phone, validation.Required),
		validation.Field(&req.Body, validation.Required, validation.Length(1, 4096)),
		validation.Field(&req.Footer, validation.Length(0, 256)),
	); err != nil {
		return pkgError.ValidationError(err.Error())
	}
	if err := validatePhoneNumber(req.Phone); err != nil {
		return err
	}
	invalid := func(s string) error { return pkgError.ValidationError(s) }
	if strings.TrimSpace(req.Body) == "" || len(req.Body) > 4096 || len(req.Footer) > 256 {
		return invalid("body is required (max 4096 bytes); footer max 256 bytes")
	}
	if (len(req.Buttons) == 0) == (len(req.Cards) == 0) {
		return invalid("provide either buttons or cards, not both")
	}
	check := func(buttons []domainSend.NativeFlowButton) error {
		if len(buttons) < 1 || len(buttons) > 10 {
			return invalid("each message/card needs 1–10 buttons")
		}
		for _, b := range buttons {
			var p map[string]any
			if len(b.Params) > 16384 || json.Unmarshal(b.Params, &p) != nil || p == nil {
				return invalid("button params must be a JSON object (max 16384 bytes)")
			}
			required := []string{"display_text"}
			switch b.Name {
			case "quick_reply":
				required = append(required, "id")
			case "cta_url":
				required = append(required, "url")
			case "cta_call":
				required = append(required, "phone_number")
			case "cta_copy":
				required = append(required, "copy_code")
			case "single_select":
				required = []string{"title"}
				sections, ok := p["sections"].([]any)
				if !ok || len(sections) == 0 {
					return invalid("single_select requires sections")
				}
				for _, value := range sections {
					section, ok := value.(map[string]any)
					if !ok {
						return invalid("single_select sections must be objects")
					}
					rows, ok := section["rows"].([]any)
					if !ok || len(rows) == 0 {
						return invalid("single_select sections require nonempty rows")
					}
					for _, value := range rows {
						row, ok := value.(map[string]any)
						if !ok {
							return invalid("single_select rows must be objects")
						}
						for _, key := range []string{"id", "title"} {
							if text, ok := row[key].(string); !ok || strings.TrimSpace(text) == "" {
								return invalid("single_select rows require nonempty string " + key)
							}
						}
					}
				}
			case "send_location":
				if len(buttons) != 1 || len(req.Cards) > 0 {
					return invalid("send_location must be the only button in a non-carousel message")
				}
				required = nil
			default:
				return invalid(fmt.Sprintf("unsupported button name %q", b.Name))
			}
			for _, key := range required {
				if v, ok := p[key].(string); !ok || strings.TrimSpace(v) == "" {
					return invalid("button params requires string " + key)
				}
			}
			if b.Name == "cta_url" {
				u, err := url.Parse(p["url"].(string))
				if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
					return invalid("cta_url requires an absolute HTTP(S) URL")
				}
			}
		}
		return nil
	}
	if len(req.Buttons) > 0 {
		return check(req.Buttons)
	}
	if len(req.Cards) < 2 || len(req.Cards) > 10 {
		return invalid("carousel needs 2–10 cards")
	}
	for _, c := range req.Cards {
		if c.ImageBase64 != "" {
			if len(c.ImageBase64) > 2800000 {
				return invalid("card image exceeds 2 MB")
			}
			data, err := base64.StdEncoding.DecodeString(c.ImageBase64)
			if err != nil {
				return invalid("card image must be base64 JPEG or PNG")
			}
			if len(data) > 2000000 || int64(len(data)) > config.WhatsappSettingMaxImageSize {
				return invalid("card image exceeds configured image limit or carousel 2 MB limit")
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || (format != "jpeg" && format != "png") || cfg.Width > 4096 || cfg.Height > 4096 {
				return invalid("card image must be JPEG/PNG up to 4096 pixels per side")
			}
		}
		if strings.TrimSpace(c.Body) == "" || len(c.Body) > 4096 || len(c.Title) > 256 || len(c.Footer) > 256 {
			return invalid("invalid carousel card text lengths")
		}
		if err := check(c.Buttons); err != nil {
			return err
		}
	}
	return nil
}
