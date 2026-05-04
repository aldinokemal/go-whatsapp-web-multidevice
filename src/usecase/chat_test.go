package usecase

import (
	"context"
	"testing"
)

func TestResolveSenderDisplayName(t *testing.T) {
	t.Run("returns me for outgoing messages", func(t *testing.T) {
		got := resolveSenderDisplayName(context.Background(), nil, "12345@s.whatsapp.net", true, nil)
		if got != "Me" {
			t.Fatalf("resolveSenderDisplayName() = %q, want %q", got, "Me")
		}
	})

	t.Run("falls back to sender jid when no client is available", func(t *testing.T) {
		const senderJID = "12345@s.whatsapp.net"

		got := resolveSenderDisplayName(context.Background(), nil, senderJID, false, nil)
		if got != senderJID {
			t.Fatalf("resolveSenderDisplayName() = %q, want %q", got, senderJID)
		}
	})

	t.Run("reuses cached display names", func(t *testing.T) {
		cache := map[string]string{
			"12345@s.whatsapp.net": "Alice",
		}

		got := resolveSenderDisplayName(context.Background(), nil, "12345@s.whatsapp.net", false, cache)
		if got != "Alice" {
			t.Fatalf("resolveSenderDisplayName() = %q, want %q", got, "Alice")
		}
	})
}
