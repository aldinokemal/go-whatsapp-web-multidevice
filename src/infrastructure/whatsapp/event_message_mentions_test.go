package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type mentionLIDStore struct {
	store.NoopStore
	phoneNumbers map[types.JID]types.JID
}

func (s *mentionLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	return s.phoneNumbers[lid], nil
}

var (
	mentionAccount = types.NewJID("628111111111", types.DefaultUserServer)
	mentionPhone   = types.NewJID("628123456789", types.DefaultUserServer)
	mentionLID     = types.NewJID("123456789012345", types.HiddenUserServer)
	unknownLID     = types.NewJID("999999999999999", types.HiddenUserServer)
)

// quietLog sets the package logger NormalizeJIDFromLID writes to, which is
// only initialised at startup, and restores it after the test.
func quietLog(t *testing.T) {
	t.Helper()
	original := log
	log = waLog.Noop
	t.Cleanup(func() { log = original })
}

func mentionClient() *whatsmeow.Client {
	lids := &mentionLIDStore{phoneNumbers: map[types.JID]types.JID{mentionLID: mentionPhone}}
	return &whatsmeow.Client{Store: &store.Device{ID: &mentionAccount, LIDs: lids, Contacts: lids}}
}

func groupMessage(msg *waE2E.Message) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:    types.NewJID("120363000000000001", types.GroupServer),
				Sender:  types.NewJID("628222222222", types.DefaultUserServer),
				IsGroup: true,
			},
			ID:        "MSG-MENTION",
			Timestamp: time.Date(2026, time.October, 17, 10, 0, 0, 0, time.UTC),
		},
		Message: msg,
	}
}

func textWithContext(text string, ci *waE2E.ContextInfo) *waE2E.Message {
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: protoString(text), ContextInfo: ci}}
}

func TestBuildEventPayloadMentions(t *testing.T) {
	quietLog(t)
	tests := []struct {
		name         string
		client       *whatsmeow.Client
		mentioned    []string
		wantMentions any
		wantLIDs     any
	}{
		{
			name:         "phone JIDs are passed through",
			client:       mentionClient(),
			mentioned:    []string{mentionPhone.String()},
			wantMentions: []string{mentionPhone.String()},
		},
		{
			name:         "a LID is resolved to its phone JID and kept as mentions_lid",
			client:       mentionClient(),
			mentioned:    []string{mentionLID.String()},
			wantMentions: []string{mentionPhone.String()},
			wantLIDs:     []string{mentionLID.String()},
		},
		{
			name:         "an unresolvable LID stays a LID",
			client:       mentionClient(),
			mentioned:    []string{unknownLID.String()},
			wantMentions: []string{unknownLID.String()},
			wantLIDs:     []string{unknownLID.String()},
		},
		{
			name:         "without a client the JIDs are passed as sent",
			client:       nil,
			mentioned:    []string{mentionLID.String(), mentionPhone.String()},
			wantMentions: []string{mentionLID.String(), mentionPhone.String()},
			wantLIDs:     []string{mentionLID.String()},
		},
		{
			name:         "duplicates are dropped and order is kept",
			client:       mentionClient(),
			mentioned:    []string{mentionPhone.String(), mentionLID.String(), mentionPhone.String()},
			wantMentions: []string{mentionPhone.String()},
			wantLIDs:     []string{mentionLID.String()},
		},
		{
			name:      "empty and unparseable entries are skipped",
			client:    mentionClient(),
			mentioned: []string{"", "not a jid@@"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := groupMessage(textWithContext("hello", &waE2E.ContextInfo{MentionedJID: tt.mentioned}))
			eventType, payload, err := buildEventPayload(context.Background(), tt.client, evt)
			assert.NoError(t, err)
			assert.Equal(t, EventTypeMessage, eventType)
			if tt.wantMentions == nil {
				assert.NotContains(t, payload, "mentions")
			} else {
				assert.Equal(t, tt.wantMentions, payload["mentions"])
			}
			if tt.wantLIDs == nil {
				assert.NotContains(t, payload, "mentions_lid")
			} else {
				assert.Equal(t, tt.wantLIDs, payload["mentions_lid"])
			}
		})
	}
}

func TestBuildEventPayloadMentionsInMediaCaption(t *testing.T) {
	quietLog(t)
	oldAutoDownload := config.WhatsappAutoDownloadMedia
	config.WhatsappAutoDownloadMedia = false
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = oldAutoDownload })

	caption := "@" + mentionLID.User + " please check"
	evt := groupMessage(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:     protoString(caption),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{mentionLID.String()}},
	}})

	_, payload, err := buildEventPayload(context.Background(), mentionClient(), evt)
	assert.NoError(t, err)
	assert.Equal(t, "@"+mentionPhone.User+" please check", payload["body"],
		"a caption's LID mention is rewritten like a text body's")
	assert.Equal(t, []string{mentionPhone.String()}, payload["mentions"])

	_, payload, err = buildEventPayload(context.Background(), nil, evt)
	assert.NoError(t, err)
	assert.Equal(t, caption, payload["body"], "without a LID store the caption is left as sent")
}

func TestBuildEventPayloadNoMentionFields(t *testing.T) {
	quietLog(t)
	_, payload, err := buildEventPayload(context.Background(), mentionClient(),
		groupMessage(&waE2E.Message{Conversation: protoString("no mentions here")}))
	assert.NoError(t, err)
	assert.NotContains(t, payload, "mentions")
	assert.NotContains(t, payload, "mentions_lid")
	assert.NotContains(t, payload, "replied_to_sender")
	assert.NotContains(t, payload, "replied_to_sender_lid")
}

func TestBuildEventPayloadRepliedToSender(t *testing.T) {
	quietLog(t)
	tests := []struct {
		name        string
		participant string
		wantSender  any
		wantLID     any
	}{
		{name: "a phone author is passed through", participant: mentionPhone.String(),
			wantSender: mentionPhone.String()},
		{name: "a LID author is resolved and kept as replied_to_sender_lid", participant: mentionLID.String(),
			wantSender: mentionPhone.String(), wantLID: mentionLID.String()},
		{name: "an unresolvable LID author stays a LID", participant: unknownLID.String(),
			wantSender: unknownLID.String(), wantLID: unknownLID.String()},
		{name: "a reply without an author has neither field", participant: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci := &waE2E.ContextInfo{StanzaID: protoString("QUOTED-1"), QuotedMessage: &waE2E.Message{
				Conversation: protoString("the original"),
			}}
			if tt.participant != "" {
				ci.Participant = protoString(tt.participant)
			}
			_, payload, err := buildEventPayload(context.Background(), mentionClient(),
				groupMessage(textWithContext("and Delhi?", ci)))
			assert.NoError(t, err)
			assert.Equal(t, "QUOTED-1", payload["replied_to_id"])
			assert.Equal(t, "the original", payload["quoted_body"])
			if tt.wantSender == nil {
				assert.NotContains(t, payload, "replied_to_sender")
			} else {
				assert.Equal(t, tt.wantSender, payload["replied_to_sender"])
			}
			if tt.wantLID == nil {
				assert.NotContains(t, payload, "replied_to_sender_lid")
			} else {
				assert.Equal(t, tt.wantLID, payload["replied_to_sender_lid"])
			}
		})
	}
}
