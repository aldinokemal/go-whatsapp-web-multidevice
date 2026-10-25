package usecase

import (
	"context"
	"testing"

	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow/types"
)

func TestToBlocklistResponse(t *testing.T) {
	assert.Equal(t, []domainUser.BlocklistItem{}, toBlocklistResponse(nil).Data)

	list := &types.Blocklist{Items: []types.BlocklistItem{
		{LID: types.NewJID("123456", types.HiddenUserServer), PN: types.NewJID("628123456789", types.DefaultUserServer)},
		{LID: types.NewJID("999", types.HiddenUserServer)},
	}}
	got := toBlocklistResponse(list).Data
	assert.Equal(t, []domainUser.BlocklistItem{
		{LID: "123456@lid", PN: "628123456789@s.whatsapp.net"},
		{LID: "999@lid"},
	}, got)
}

func TestValidateBlockRequiresPhone(t *testing.T) {
	assert.Error(t, validations.ValidateBlock(context.Background(), domainUser.BlockRequest{}))
	assert.NoError(t, validations.ValidateBlock(context.Background(), domainUser.BlockRequest{Phone: "628123456789@s.whatsapp.net"}))
}
