package validations

import (
	"context"
	"testing"

	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidateEditLabel(t *testing.T) {
	negative := int32(-1)
	tests := []struct {
		name    string
		request domainLabel.EditLabelRequest
		err     any
	}{
		{"create a Business label", domainLabel.EditLabelRequest{LabelID: "30", Name: "Paid"}, nil},
		{"create a chat list", domainLabel.EditLabelRequest{LabelID: "30", Name: "Family", Type: "custom"}, nil},
		{"delete without a name", domainLabel.EditLabelRequest{LabelID: "30", Deleted: true}, nil},
		{"missing label_id", domainLabel.EditLabelRequest{Name: "Paid"}, pkgError.ValidationError("label_id: cannot be blank.")},
		{"missing name", domainLabel.EditLabelRequest{LabelID: "30", Name: "  "}, pkgError.ValidationError("name: cannot be blank.")},
		{"negative color", domainLabel.EditLabelRequest{LabelID: "30", Name: "Paid", Color: &negative}, pkgError.ValidationError("color: must be no less than 0.")},
		{"negative order", domainLabel.EditLabelRequest{LabelID: "30", Name: "Paid", OrderIndex: &negative}, pkgError.ValidationError("order_index: must be no less than 0.")},
		{"system list type", domainLabel.EditLabelRequest{LabelID: "30", Name: "Paid", Type: "FAVORITES"}, pkgError.ValidationError(`type: must be empty or "CUSTOM".`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEditLabel(context.Background(), &tt.request)
			assert.Equal(t, tt.err, err)
		})
	}

	request := domainLabel.EditLabelRequest{LabelID: " 30 ", Name: " Family ", Type: " custom "}
	assert.NoError(t, ValidateEditLabel(context.Background(), &request))
	assert.Equal(t, domainLabel.EditLabelRequest{LabelID: "30", Name: "Family", Type: "CUSTOM"}, request)
}

func TestValidateLabelChat(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name    string
		request domainLabel.LabelChatRequest
		err     any
	}{
		{"add", domainLabel.LabelChatRequest{ChatJID: "6289685028129@s.whatsapp.net", LabelID: "30", Labeled: &on}, nil},
		{"remove", domainLabel.LabelChatRequest{ChatJID: "6289685028129@s.whatsapp.net", LabelID: "30", Labeled: &off}, nil},
		{"missing labeled", domainLabel.LabelChatRequest{ChatJID: "6289685028129@s.whatsapp.net", LabelID: "30"}, pkgError.ValidationError("labeled: is required.")},
		{"missing label_id", domainLabel.LabelChatRequest{ChatJID: "6289685028129@s.whatsapp.net", Labeled: &on}, pkgError.ValidationError("label_id: cannot be blank.")},
		{"missing chat_jid", domainLabel.LabelChatRequest{LabelID: "30", Labeled: &on}, pkgError.ValidationError("chat_jid: cannot be blank.")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateLabelChat(context.Background(), &tt.request)
			assert.Equal(t, tt.err, err)
		})
	}
}
