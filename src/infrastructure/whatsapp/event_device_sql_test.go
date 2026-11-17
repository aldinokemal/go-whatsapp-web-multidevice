package whatsapp_test

import (
	"context"
	"database/sql"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domain "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	storage "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	wa "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"path/filepath"
	"testing"
	"time"
)

func eventSQLFixture(t *testing.T) (*sql.DB, domain.IChatStorageRepository, <-chan string) {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "events.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	repo := storage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())
	oldURLs, oldEvents := config.WhatsappWebhook, config.WhatsappWebhookEvents
	config.WhatsappWebhook = []string{"https://global.test/hook"}
	config.WhatsappWebhookEvents = nil
	t.Cleanup(func() { config.WhatsappWebhook, config.WhatsappWebhookEvents = oldURLs, oldEvents })
	got := make(chan string, 8)
	wa.CaptureDeviceEventsForTest(t, repo, func(_ map[string]any, url string, _ *domain.DeviceWebhookConfig) { got <- url })
	return db, repo, got
}

func TestDeviceEventSQLRepairedSlotKeepsWebhook(t *testing.T) {
	_, repo, got := eventSQLFixture(t)
	const slot = "628123:3@s.whatsapp.net"
	ownURL, siblingURL := "https://own.test/hook", "https://sibling.test/hook"
	require.NoError(t, repo.SaveDeviceRecord(&domain.DeviceRecord{DeviceID: slot}))
	require.NoError(t, repo.SetDeviceWebhookConfig(slot, &domain.DeviceWebhookConfig{WebhookURL: &ownURL}))
	require.NoError(t, repo.SaveDeviceRecord(&domain.DeviceRecord{DeviceID: "sibling", JID: "628123@s.whatsapp.net", ADJID: "628123:9@s.whatsapp.net", WebhookURL: &siblingURL}))
	jid := types.NewADJID("628123", 0, 7)
	inst := wa.NewDeviceInstance(slot, whatsmeow.NewClient(&store.Device{ID: &jid}, nil), repo)
	done := make(chan struct{})
	go func() {
		wa.HandleDeviceEventForTest(context.Background(), inst, &events.TemporaryBan{Code: events.TempBanBlockedByUsers})
		close(done)
	}()
	select {
	case <-websocket.Broadcast:
	case <-time.After(3 * time.Second):
		t.Fatal("missing broadcast")
	}
	<-done
	select {
	case url := <-got:
		require.Equal(t, ownURL, url)
	case <-time.After(3 * time.Second):
		t.Fatal("missing webhook")
	}
}

func TestDeviceEventSQLBlockedLookupDoesNotHoldLogout(t *testing.T) {
	db, repo, got := eventSQLFixture(t)
	require.NoError(t, repo.SaveDeviceRecord(&domain.DeviceRecord{DeviceID: "slot", JID: "628123@s.whatsapp.net"}))
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	defer conn.Close()
	jid := types.NewADJID("628123", 0, 7)
	inst := wa.NewDeviceInstance("slot", whatsmeow.NewClient(&store.Device{ID: &jid}, nil), repo)
	reset, done := make(chan struct{}), make(chan struct{})
	inst.SetOnLoggedOut(func(string) { inst.ResetClient(); close(reset) })
	go func() { wa.HandleDeviceEventForTest(context.Background(), inst, &events.LoggedOut{}); close(done) }()
	select {
	case <-reset:
	case <-time.After(2 * time.Second):
		conn.Close()
		<-reset
		<-websocket.Broadcast
		<-done
		<-got
		t.Fatal("blocked SQL lookup held logout beyond its budget")
	}
	<-websocket.Broadcast
	<-done
	select {
	case url := <-got:
		require.Equal(t, "https://global.test/hook", url)
	case <-time.After(time.Second):
		t.Fatal("missing fallback webhook")
	}
	require.Empty(t, inst.JID())
}

func TestDeviceEventSQLWebhookPolicy(t *testing.T) {
	for _, tc := range []struct{ name, url, events, want string }{
		{"configured", "https://own.test/hook", "device.temporary_ban", "https://own.test/hook"},
		{"filtered", "https://own.test/hook", "message", ""},
		{"empty URL uses global policy", "", "message", "https://global.test/hook"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repo, _ := eventSQLFixture(t)
			require.NoError(t, repo.SaveDeviceRecord(&domain.DeviceRecord{DeviceID: "slot"}))
			require.NoError(t, repo.SetDeviceWebhookConfig("slot", &domain.DeviceWebhookConfig{WebhookURL: &tc.url, WebhookEvents: tc.events, WebhookSecret: "device-secret", WebhookInsecureSkipVerify: true}))
			type delivery struct {
				url string
				cfg *domain.DeviceWebhookConfig
			}
			got := make(chan delivery, 1)
			wa.CaptureDeviceEventsForTest(t, repo, func(_ map[string]any, url string, cfg *domain.DeviceWebhookConfig) { got <- delivery{url, cfg} })
			inst := wa.NewDeviceInstance("slot", nil, repo)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			done := make(chan struct{})
			go func() {
				wa.HandleDeviceEventForTest(ctx, inst, &events.TemporaryBan{Code: events.TempBanBlockedByUsers})
				close(done)
			}()
			<-websocket.Broadcast
			<-done
			select {
			case d := <-got:
				require.Equal(t, tc.want, d.url)
				if tc.url == "" {
					require.Nil(t, d.cfg)
				} else {
					require.Equal(t, "device-secret", d.cfg.WebhookSecret)
					require.True(t, d.cfg.WebhookInsecureSkipVerify)
				}
			case <-time.After(100 * time.Millisecond):
				require.Empty(t, tc.want, "missing configured delivery")
			}
		})
	}
}
