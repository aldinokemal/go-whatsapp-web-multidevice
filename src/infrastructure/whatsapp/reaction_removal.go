package whatsapp

import (
	"context"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow/types/events"
)

type removedReactionCtxKey struct{}

// withRemovedReactionEmoji captures the reaction before storage deletes it.
func withRemovedReactionEmoji(ctx context.Context, evt *events.Message, repo domainChatStorage.IChatStorageRepository) context.Context {
	if repo == nil || evt == nil || evt.Message == nil {
		return ctx
	}
	reaction := utils.UnwrapMessage(evt.Message).GetReactionMessage()
	if reaction == nil || reaction.GetText() != "" {
		return ctx
	}
	stored, err := repo.GetReactionForEvent(ctx, evt)
	if err != nil {
		log.Warnf("Failed to resolve removed reaction for %s: %v", sanitizeLogText(evt.Info.ID), err)
		return ctx
	}
	if stored == nil || stored.Emoji == "" {
		return ctx
	}
	return context.WithValue(ctx, removedReactionCtxKey{}, stored.Emoji)
}

func removedReactionEmojiFromContext(ctx context.Context) string {
	emoji, _ := ctx.Value(removedReactionCtxKey{}).(string)
	return emoji
}
