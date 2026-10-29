package usecase

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
)

// TestSendRequestExtrasPinsTheMessageID: an id set on the send context is the
// id handed to whatsmeow; without one whatsmeow chooses.
func TestSendRequestExtrasPinsTheMessageID(t *testing.T) {
	if extras := sendRequestExtras(context.Background()); len(extras) != 0 {
		t.Fatalf("extras = %+v, want none without a pinned id", extras)
	}
	ctx := whatsapp.ContextWithSendMessageID(context.Background(), "3EB0AAAAAAAAAAAAAAAAAA")
	extras := sendRequestExtras(ctx)
	if len(extras) != 1 || string(extras[0].ID) != "3EB0AAAAAAAAAAAAAAAAAA" {
		t.Fatalf("extras = %+v, want the pinned id", extras)
	}
}
