package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A reservation stub (chatwoot_message_id == 0) is rolled back on every failure
// path of a forward, so one that stops being touched belongs to a process killed
// mid-forward. Left in place it answers "already in flight" to every redelivery
// and hides the message from Chatwoot forever, hence the age-based reclaim.
// Finished links and stubs of forwards still running must survive it.
func TestDeleteStaleChatwootMessageLinkReservations(t *testing.T) {
	deviceID := "628987654321@s.whatsapp.net"
	waID := func(suffix string) string { return "3EB0C127D7BACC83D6" + suffix }

	seed := func(t *testing.T, repo *SQLiteRepository, id string, chatwootMessageID int, updatedAt time.Time) {
		t.Helper()
		require.NoError(t, repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
			DeviceID:          deviceID,
			WhatsAppMessageID: id,
			WhatsAppChatJID:   "628123456789@s.whatsapp.net",
			ChatwootMessageID: chatwootMessageID,
			Direction:         "incoming",
		}))
		// updated_at is stamped with time.Now() by the upsert; age the row by hand
		// so the sweep has something older than its cutoff to find.
		_, err := repo.db.Exec(`
			UPDATE chatwoot_message_links SET updated_at = ?
			WHERE device_id = ? AND wa_message_id = ?
		`, updatedAt, deviceID, id)
		require.NoError(t, err)
	}

	now := time.Now()
	cases := []struct {
		name              string
		chatwootMessageID int
		updatedAt         time.Time
		wantReclaimed     bool
	}{
		{"orphaned stub is reclaimed", 0, now.Add(-time.Hour), true},
		{"stub of a forward still running survives", 0, now, false},
		{"stub exactly at the cutoff survives", 0, now.Add(-30 * time.Minute), false},
		{"finished link of the same age survives", 4242, now.Add(-time.Hour), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			seed(t, repo, waID("01"), tc.chatwootMessageID, tc.updatedAt)

			removed, err := repo.DeleteStaleChatwootMessageLinkReservations(now.Add(-30 * time.Minute))
			require.NoError(t, err)

			link, err := repo.GetChatwootMessageLinkByWhatsAppID(deviceID, waID("01"))
			require.NoError(t, err)
			if tc.wantReclaimed {
				assert.Equal(t, int64(1), removed)
				assert.Nil(t, link, "the orphaned stub must be gone so a redelivery can reserve again")
				return
			}
			assert.Zero(t, removed)
			assert.NotNil(t, link)
		})
	}

	t.Run("reclaims across devices and reports the count", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		seed(t, repo, waID("02"), 0, now.Add(-time.Hour))
		seed(t, repo, waID("03"), 0, now.Add(-2*time.Hour))
		seed(t, repo, waID("04"), 7, now.Add(-time.Hour))

		removed, err := repo.DeleteStaleChatwootMessageLinkReservations(now.Add(-30 * time.Minute))
		require.NoError(t, err)
		assert.Equal(t, int64(2), removed)

		survivor, err := repo.GetChatwootMessageLinkByWhatsAppID(deviceID, waID("04"))
		require.NoError(t, err)
		assert.NotNil(t, survivor)
	})
}
