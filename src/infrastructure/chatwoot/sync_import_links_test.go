package chatwoot

import (
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot/pgimport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type importLinkTestRepo struct {
	domainChatStorage.IChatStorageRepository
	upserted    []string
	placeholder map[string]bool
}

func newImportLinkTestRepo() *importLinkTestRepo {
	return &importLinkTestRepo{placeholder: make(map[string]bool)}
}

func (r *importLinkTestRepo) UpsertChatwootMessageLink(link *domainChatStorage.ChatwootMessageLink) error {
	r.upserted = append(r.upserted, link.WhatsAppMessageID)
	return nil
}

func (r *importLinkTestRepo) SetChatwootLinkViewOncePlaceholder(_, waMessageID string, isPlaceholder bool) error {
	r.placeholder[waMessageID] = isPlaceholder
	return nil
}

// The direct importer probes Chatwoot by source_id and returns a link for rows
// it merely found, without touching the message they describe. Only the links
// whose content it actually wrote may restate the view-once discriminator: on a
// skip, the Chatwoot message still shows whatever it showed, and the stored flag
// is whatever the live path has since claimed for it.
func TestStoreChatwootImportLinksPreservesPlaceholderOnSkips(t *testing.T) {
	const deviceID = "628987654321@s.whatsapp.net"

	link := func(waMessageID string, isPlaceholder bool) domainChatStorage.ChatwootMessageLink {
		return domainChatStorage.ChatwootMessageLink{
			DeviceID:              deviceID,
			WhatsAppMessageID:     waMessageID,
			WhatsAppChatJID:       "628123456789@s.whatsapp.net",
			ChatwootMessageID:     99,
			IsViewOncePlaceholder: isPlaceholder,
		}
	}

	cases := []struct {
		name            string
		result          *pgimport.ImportResult
		wantUpserted    []string
		wantPlaceholder map[string]bool
	}{
		{
			name: "written placeholder stamps the discriminator",
			result: &pgimport.ImportResult{
				Links:             []domainChatStorage.ChatwootMessageLink{link("wa-written", true)},
				WrittenMessageIDs: []string{"wa-written"},
			},
			wantUpserted:    []string{"wa-written"},
			wantPlaceholder: map[string]bool{"wa-written": true},
		},
		{
			name: "written content clears it",
			result: &pgimport.ImportResult{
				Links:             []domainChatStorage.ChatwootMessageLink{link("wa-written", false)},
				WrittenMessageIDs: []string{"wa-written"},
			},
			wantUpserted:    []string{"wa-written"},
			wantPlaceholder: map[string]bool{"wa-written": false},
		},
		{
			name: "skipped row keeps the stored state",
			result: &pgimport.ImportResult{
				Links: []domainChatStorage.ChatwootMessageLink{link("wa-skipped", false)},
			},
			wantUpserted:    []string{"wa-skipped"},
			wantPlaceholder: map[string]bool{},
		},
		{
			name: "mixed run separates the two",
			result: &pgimport.ImportResult{
				Links: []domainChatStorage.ChatwootMessageLink{
					link("wa-written", true),
					link("wa-skipped", false),
				},
				WrittenMessageIDs: []string{"wa-written"},
			},
			wantUpserted:    []string{"wa-written", "wa-skipped"},
			wantPlaceholder: map[string]bool{"wa-written": true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newImportLinkTestRepo()
			svc := NewSyncService(&Client{AccountID: 1, InboxID: 2}, repo)

			require.NoError(t, svc.storeChatwootImportLinks(tc.result))

			assert.Equal(t, tc.wantUpserted, repo.upserted, "every link is still mapped")
			assert.Equal(t, tc.wantPlaceholder, repo.placeholder,
				"only links this import actually wrote may restate the discriminator")
		})
	}

	t.Run("no storage configured is a no-op", func(t *testing.T) {
		svc := NewSyncService(&Client{AccountID: 1, InboxID: 2}, nil)
		assert.NoError(t, svc.storeChatwootImportLinks(&pgimport.ImportResult{
			Links: []domainChatStorage.ChatwootMessageLink{link("wa-written", true)},
		}))
	})
}
