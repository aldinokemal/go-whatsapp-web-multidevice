package usecase

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
)

// chatDisplayName keeps the existing table-driven fallback coverage focused on
// the production resolver after the local helper moved to infrastructure.
func chatDisplayName(jid, name string) string {
	ctx := context.Background()
	return whatsapp.NewChatDisplayNameResolver(ctx, nil).Resolve(ctx, jid, name)
}
