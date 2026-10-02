package usecase

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func newLinkPreviewPage(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Countdown</title><meta property="og:title" content="Countdown"><meta name="description" content="Live timer"></head><body></body></html>`))
	}))
	t.Cleanup(server.Close)
	return server.URL + "/timer"
}

// editLinkTestService wraps the shared message test service and captures the
// edited content that UpdateMessage hands to WhatsApp.
func editLinkTestService(t *testing.T) (serviceMessage, func() *waE2E.Message, context.Context, func(string) string) {
	t.Helper()
	service, repo, ctx := newMessageActionTestService(t, nil)
	var sent *waE2E.Message
	service.sendMessageFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error) {
		sent = message
		return whatsmeow.SendResponse{ID: "edit-event-1"}, nil
	}
	edited := func() *waE2E.Message {
		require.NotNil(t, sent)
		return sent.GetEditedMessage().GetMessage().GetProtocolMessage().GetEditedMessage()
	}
	stored := func(id string) string {
		msg, err := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", id)
		require.NoError(t, err)
		require.NotNil(t, msg)
		return msg.Content
	}
	return service, edited, ctx, stored
}

func TestUpdateMessageWithLinkSendsPreviewAndStoresSentText(t *testing.T) {
	link := newLinkPreviewPage(t)
	service, edited, ctx, stored := editLinkTestService(t)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "09:59",
		Link:      link,
	})
	require.NoError(t, err)

	ext := edited().GetExtendedTextMessage()
	require.NotNil(t, ext, "a link edit must be sent as an ExtendedTextMessage")
	assert.Equal(t, "09:59\n"+link, ext.GetText())
	assert.Equal(t, link, ext.GetMatchedText())
	assert.Equal(t, "Countdown", ext.GetTitle())
	assert.Equal(t, "Live timer", ext.GetDescription())
	assert.Equal(t, "09:59\n"+link, stored("message-1"), "stored copy must match the text recipients see")
}

func TestUpdateMessageWithLinkDoesNotDuplicatePresentLink(t *testing.T) {
	link := newLinkPreviewPage(t)
	service, edited, ctx, stored := editLinkTestService(t)

	text := "timer: " + link
	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   text,
		Link:      link,
	})
	require.NoError(t, err)

	assert.Equal(t, text, edited().GetExtendedTextMessage().GetText())
	assert.Equal(t, text, stored("message-1"))
}

func TestUpdateMessageWithLinkAppendsWhenOnlyAPrefixIsPresent(t *testing.T) {
	link := newLinkPreviewPage(t)
	service, edited, ctx, _ := editLinkTestService(t)

	text := "see " + link + ".evil"
	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   text,
		Link:      link,
	})
	require.NoError(t, err)

	assert.Equal(t, text+"\n"+link, edited().GetExtendedTextMessage().GetText())
}

func TestUpdateMessageWithoutLinkStaysPlainText(t *testing.T) {
	service, edited, ctx, _ := editLinkTestService(t)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "plain edit",
	})
	require.NoError(t, err)

	assert.Equal(t, "plain edit", edited().GetConversation())
	assert.Nil(t, edited().GetExtendedTextMessage())
}

func TestContainsLinkToken(t *testing.T) {
	const link = "https://example.com"
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"bare", link, true},
		{"after words", "see " + link, true},
		{"trailing period", "See " + link + ".", true},
		{"trailing comma", link + ", then", true},
		{"parentheses", "Visit (" + link + ")", true},
		{"parentheses and period", "Visit (" + link + ").", true},
		{"angle brackets", "<" + link + ">", true},
		{"quotes", "\"" + link + "\"", true},
		{"markdown link", "[label](" + link + ")", true},
		{"tight prose", "See(" + link + ")", true},
		{"curly double quotes", "“" + link + "”", true},
		{"curly single quotes in parentheses", "(‘" + link + "’)", true},
		{"curly quote inside another url", "https://evil.test/“" + link + "”", false},
		{"own line", "09:59\n" + link, true},
		{"longer host", link + ".evil", false},
		{"longer host in parentheses", "(" + link + ".evil)", false},
		{"path continuation", link + "/page", false},
		{"embedded in another url", "https://evil.test/?u=" + link, false},
		{"attached to text", "x=" + link, false},
		{"parenthesized inside another url", "https://evil.test/(" + link + ")", false},
		{"bracketed after another url", "https://evil.test/[" + link + "]", false},
		{"absent", "no link here", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, containsLinkToken(tt.text, link))
		})
	}
}

func TestUpdateMessageWithLinkDoesNotDuplicatePunctuatedLink(t *testing.T) {
	link := newLinkPreviewPage(t)
	service, edited, ctx, stored := editLinkTestService(t)

	text := "Visit (" + link + ")."
	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   text,
		Link:      link,
	})
	require.NoError(t, err)

	assert.Equal(t, text, edited().GetExtendedTextMessage().GetText())
	assert.Equal(t, text, stored("message-1"))
}
