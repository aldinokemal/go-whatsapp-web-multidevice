package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
)

// TestSendRequestExtrasPinsTheMessageID: an id set on the send context is the
// id handed to whatsmeow; without one whatsmeow chooses. A caller extra, such
// as interactive additional nodes, stays on that same value because whatsmeow
// accepts only one SendRequestExtra.
func TestSendRequestExtrasPinsTheMessageID(t *testing.T) {
	if extras := sendRequestExtras(context.Background()); len(extras) != 0 {
		t.Fatalf("extras = %+v, want none without a pinned id", extras)
	}
	ctx := whatsapp.ContextWithSendMessageID(context.Background(), "3EB0AAAAAAAAAAAAAAAAAA")
	extras := sendRequestExtras(ctx)
	if len(extras) != 1 || extras[0].ID != "3EB0AAAAAAAAAAAAAAAAAA" {
		t.Fatalf("extras = %+v, want the pinned id", extras)
	}

	nodes := []waBinary.Node{{Tag: "biz"}}
	caller := whatsmeow.SendRequestExtra{Timeout: 5 * time.Second, AdditionalNodes: &nodes}
	merged := sendRequestExtras(ctx, caller)
	if len(merged) != 1 || merged[0].ID != "3EB0AAAAAAAAAAAAAAAAAA" || merged[0].Timeout != 5*time.Second || merged[0].AdditionalNodes != &nodes {
		t.Fatalf("merged = %+v, want pinned id plus the caller extra", merged)
	}
	if caller.ID != "" {
		t.Fatalf("caller id = %q, merging must not mutate the caller", caller.ID)
	}

	caller.ID = "3EB0CALLERCHOICE"
	merged = sendRequestExtras(ctx, caller)
	if len(merged) != 1 || merged[0].ID != "3EB0CALLERCHOICE" || merged[0].AdditionalNodes != &nodes {
		t.Fatalf("merged = %+v, want the caller id to win", merged)
	}
}
