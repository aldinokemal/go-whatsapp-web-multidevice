package usecase

import (
	"context"
	"encoding/json"
	"fmt"

	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// whatsmeow has no public helper for the channel directory, so these GraphQL
// ("mex") queries are sent through its low-level SendMexIQ. The IDs and input
// shapes come from WhatsApp Web (WAWebMexFetchNewsletterDirectoryListJob and
// WAWebMexFetchNewsletterDirectorySearchResultsJob) and may change without notice.
const (
	queryNewsletterDirectoryList   = "26125047313831973"
	queryNewsletterDirectorySearch = "26301059626252132"
	// Same query whatsmeow's GetNewsletterInfo uses, sent without viewer metadata.
	queryFetchNewsletter = "6563316087068696"
)

type newsletterMexClient interface {
	SendMexIQ(ctx context.Context, queryID string, variables any) (json.RawMessage, error)
}

func newsletterMexClientOf(client *whatsmeow.Client) newsletterMexClient {
	//lint:ignore SA1019 whatsmeow has no public API for the directory or metadata without viewer fields.
	return client.DangerousInternals()
}

func (service serviceNewsletter) Directory(ctx context.Context, request domainNewsletter.DirectoryRequest) (response domainNewsletter.DirectoryResponse, err error) {
	if err = validations.ValidateNewsletterDirectory(ctx, &request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	return fetchNewsletterDirectory(ctx, newsletterMexClientOf(client), request)
}

func fetchNewsletterDirectory(ctx context.Context, client newsletterMexClient, request domainNewsletter.DirectoryRequest) (response domainNewsletter.DirectoryResponse, err error) {
	queryID, variables := newsletterDirectoryQuery(request)
	data, err := client.SendMexIQ(ctx, queryID, variables)
	if err != nil {
		return response, fmt.Errorf("failed to query newsletter directory: %w", err)
	}
	return parseNewsletterDirectory(data)
}

func newsletterDirectoryQuery(request domainNewsletter.DirectoryRequest) (string, map[string]any) {
	categories := request.Categories
	if categories == nil {
		categories = []string{}
	}

	input := map[string]any{
		"limit": request.Limit,
	}
	if request.Cursor != "" {
		input["start_cursor"] = request.Cursor
	}

	if request.SearchText != "" {
		input["search_text"] = request.SearchText
		input["categories"] = categories
		return queryNewsletterDirectorySearch, map[string]any{"input": input, "fetch_status_metadata": false}
	}

	countryCodes := request.CountryCodes
	if countryCodes == nil {
		countryCodes = []string{}
	}
	input["view"] = request.View
	input["filters"] = map[string]any{
		"country_codes": countryCodes,
		"categories":    categories,
	}
	return queryNewsletterDirectoryList, map[string]any{"input": input, "fetch_status_metadata": false}
}

type newsletterDirectoryPage struct {
	Result   []types.NewsletterMetadata `json:"result"`
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"page_info"`
}

func parseNewsletterDirectory(data json.RawMessage) (response domainNewsletter.DirectoryResponse, err error) {
	var root map[string]json.RawMessage
	if err = json.Unmarshal(data, &root); err != nil {
		return response, fmt.Errorf("failed to decode newsletter directory: %w", err)
	}

	for _, raw := range root {
		var page newsletterDirectoryPage
		if err = json.Unmarshal(raw, &page); err != nil {
			return response, fmt.Errorf("failed to decode newsletter directory page: %w", err)
		}
		response.Data = page.Result
		response.HasMore = page.PageInfo.HasNextPage
		if response.HasMore {
			response.NextCursor = page.PageInfo.EndCursor
		}
		break
	}
	if response.Data == nil {
		response.Data = []types.NewsletterMetadata{}
	}
	return response, nil
}

func fetchNewsletterInfoWithoutViewer(ctx context.Context, client newsletterMexClient, jid types.JID) (*types.NewsletterMetadata, error) {
	data, err := client.SendMexIQ(ctx, queryFetchNewsletter, map[string]any{
		"fetch_creation_time":   true,
		"fetch_full_image":      true,
		"fetch_viewer_metadata": false,
		"input": map[string]any{
			"key":  jid.String(),
			"type": types.NewsletterKeyTypeJID,
		},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Newsletter *types.NewsletterMetadata `json:"xwa2_newsletter"`
	}
	if err = json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode newsletter info: %w", err)
	}
	return resp.Newsletter, nil
}
