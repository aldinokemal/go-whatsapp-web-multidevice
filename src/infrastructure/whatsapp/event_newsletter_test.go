package whatsapp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestNewsletterLiveUpdateBodyCountersOnly(t *testing.T) {
	evt := &events.NewsletterLiveUpdate{
		JID:  types.NewJID("120363123456789", types.NewsletterServer),
		Time: time.Date(2026, 10, 28, 17, 36, 7, 0, time.UTC),
		Messages: []*types.NewsletterMessage{{
			MessageServerID: 1828,
			ForwardsCount:   19056,
			ReactionCounts:  map[string]int{"❤️": 16008},
		}},
	}

	body := newsletterLiveUpdateBody(evt, "628123@s.whatsapp.net")

	assert.Equal(t, "newsletter.message", body["event"])
	assert.Equal(t, "2026-10-28T17:36:07Z", body["timestamp"])
	assert.Equal(t, "628123@s.whatsapp.net", body["device_id"])
	payload := body["payload"].(map[string]any)
	assert.Equal(t, "120363123456789@newsletter", payload["newsletter_id"])
	messages := payload["messages"].([]map[string]any)
	require.Len(t, messages, 1)
	assert.Equal(t, map[string]any{
		"server_id":       types.MessageServerID(1828),
		"forwards_count":  19056,
		"reaction_counts": map[string]int{"❤️": 16008},
	}, messages[0])
}

func TestNewsletterLiveUpdateBodyKeepsMessageFields(t *testing.T) {
	evt := &events.NewsletterLiveUpdate{
		JID: types.NewJID("120363123456789", types.NewsletterServer),
		Messages: []*types.NewsletterMessage{{
			MessageServerID: 7,
			MessageID:       "3A74781EA56A87F8293B",
			Type:            "text",
			Timestamp:       time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC),
			ViewsCount:      10,
		}},
	}

	messages := newsletterLiveUpdateBody(evt, "")["payload"].(map[string]any)["messages"].([]map[string]any)
	assert.Equal(t, "3A74781EA56A87F8293B", messages[0]["message_id"])
	assert.Equal(t, "text", messages[0]["type"])
	assert.Equal(t, "2026-10-28T15:00:00Z", messages[0]["timestamp"])
	assert.Equal(t, 10, messages[0]["views_count"])
}
