package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

type fakeMexClient struct {
	queryID   string
	variables any
	response  json.RawMessage
	err       error
}

func (f *fakeMexClient) SendMexIQ(_ context.Context, queryID string, variables any) (json.RawMessage, error) {
	f.queryID = queryID
	f.variables = variables
	return f.response, f.err
}

func TestNewsletterDirectoryQueryList(t *testing.T) {
	queryID, variables := newsletterDirectoryQuery(domainNewsletter.DirectoryRequest{
		View: "TRENDING", Limit: 10, Cursor: "abc", CountryCodes: []string{"BR"},
	})
	assert.Equal(t, queryNewsletterDirectoryList, queryID)
	input := variables["input"].(map[string]any)
	assert.Equal(t, "TRENDING", input["view"])
	assert.Equal(t, 10, input["limit"])
	assert.Equal(t, "abc", input["start_cursor"])
	assert.Equal(t, map[string]any{"country_codes": []string{"BR"}, "categories": []string{}}, input["filters"])
	assert.Equal(t, false, variables["fetch_status_metadata"])
}

func TestNewsletterDirectoryQuerySearch(t *testing.T) {
	queryID, variables := newsletterDirectoryQuery(domainNewsletter.DirectoryRequest{
		SearchText: "news", Categories: []string{"NEWS"}, CountryCodes: []string{"BR"}, Limit: 5,
	})
	assert.Equal(t, queryNewsletterDirectorySearch, queryID)
	input := variables["input"].(map[string]any)
	assert.Equal(t, "news", input["search_text"])
	assert.Equal(t, []string{"NEWS"}, input["categories"])
	assert.NotContains(t, input, "filters")
	assert.NotContains(t, input, "view")
}

func TestFetchNewsletterInfoWithoutViewer(t *testing.T) {
	client := &fakeMexClient{response: json.RawMessage(`{"xwa2_newsletter":{"id":"120363123456789@newsletter","thread_metadata":{"name":{"text":"News"},"subscribers_count":"10"}}}`)}
	jid := types.NewJID("120363123456789", types.NewsletterServer)

	metadata, err := fetchNewsletterInfoWithoutViewer(context.Background(), client, jid)
	require.NoError(t, err)
	assert.Equal(t, "News", metadata.ThreadMeta.Name.Text)
	assert.Equal(t, queryFetchNewsletter, client.queryID)
	assert.Equal(t, false, client.variables.(map[string]any)["fetch_viewer_metadata"])
}

func TestFetchNewsletterDirectory(t *testing.T) {
	client := &fakeMexClient{response: json.RawMessage(`{"xwa2_newsletters_directory_list":{
		"page_info":{"hasNextPage":true,"endCursor":"next"},
		"result":[{"id":"120363123456789@newsletter","state":{"type":"active"},
			"thread_metadata":{"name":{"text":"News"},"subscribers_count":"1234","verification":"VERIFIED","invite":"0029Va"}}]}}`)}

	response, err := fetchNewsletterDirectory(context.Background(), client, domainNewsletter.DirectoryRequest{View: "POPULAR", Limit: 1})
	require.NoError(t, err)
	require.Len(t, response.Data, 1)
	assert.Equal(t, "120363123456789@newsletter", response.Data[0].ID.String())
	assert.Equal(t, "News", response.Data[0].ThreadMeta.Name.Text)
	assert.Equal(t, 1234, response.Data[0].ThreadMeta.SubscriberCount)
	assert.True(t, response.HasMore)
	assert.Equal(t, "next", response.NextCursor)
}

func TestFetchNewsletterDirectoryEmptyAndErrors(t *testing.T) {
	client := &fakeMexClient{response: json.RawMessage(`{"xwa2_newsletters_directory_list":{"page_info":{"hasNextPage":false},"result":[]}}`)}
	response, err := fetchNewsletterDirectory(context.Background(), client, domainNewsletter.DirectoryRequest{View: "NEW", Limit: 5})
	require.NoError(t, err)
	assert.Empty(t, response.Data)
	assert.False(t, response.HasMore)
	assert.Empty(t, response.NextCursor)

	client = &fakeMexClient{err: errors.New("graphql error")}
	_, err = fetchNewsletterDirectory(context.Background(), client, domainNewsletter.DirectoryRequest{View: "NEW", Limit: 5})
	require.ErrorContains(t, err, "graphql error")
}
