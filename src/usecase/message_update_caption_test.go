package usecase

import (
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateMessageWithImageMediaTypeEditsCaption(t *testing.T) {
	service, edited, ctx, stored := editLinkTestService(t)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "new caption",
		MediaType: "image",
	})
	require.NoError(t, err)

	image := edited().GetImageMessage()
	require.NotNil(t, image, "an image caption edit must be sent as an ImageMessage")
	assert.Equal(t, "new caption", image.GetCaption())
	assert.Empty(t, edited().GetConversation())
	assert.Equal(t, "new caption", stored("message-1"))
}

func TestUpdateMessageWithDocumentMediaTypeEditsCaption(t *testing.T) {
	service, edited, ctx, stored := editLinkTestService(t)

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1",
		Phone:     "628123456789@s.whatsapp.net",
		Message:   "new caption",
		MediaType: "document",
	})
	require.NoError(t, err)

	document := edited().GetDocumentMessage()
	require.NotNil(t, document, "a document caption edit must be sent as a DocumentMessage")
	assert.Equal(t, "new caption", document.GetCaption())
	assert.Empty(t, edited().GetConversation())
	assert.Equal(t, "new caption", stored("message-1"))
}
