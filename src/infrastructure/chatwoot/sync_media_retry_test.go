package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
)

// historyMediaFixture imports one stored image whose first download fails with
// downloadErr, and records the message Chatwoot receives.
type historyMediaFixture struct {
	svc         *SyncService
	msg         *domainChatStorage.Message
	tempDir     string
	content     string
	attachments []string
}

func newHistoryMediaFixture(t *testing.T, downloadErr error) *historyMediaFixture {
	t.Helper()
	f := &historyMediaFixture{
		tempDir: t.TempDir(),
		msg: &domainChatStorage.Message{
			ID:        "wa-expired",
			DeviceID:  "device-a@s.whatsapp.net",
			ChatJID:   "628123456789@s.whatsapp.net",
			MediaType: "image",
			URL:       "https://mmg.whatsapp.net/v/t62.7118-24/expired.enc?oh=old",
			MediaKey:  []byte("key"),
			Timestamp: time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
		},
	}
	// The recovered file is stored in os.TempDir().
	t.Setenv("TMPDIR", f.tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			f.content = r.FormValue("content")
			for _, header := range r.MultipartForm.File["attachments[]"] {
				file, _ := header.Open()
				data, _ := io.ReadAll(file)
				f.attachments = append(f.attachments, string(data))
			}
		} else {
			var body CreateMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			f.content = body.Content
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":888}`))
	}))
	t.Cleanup(server.Close)

	f.svc = NewSyncService(&Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}, newChatwootSyncLinkTestRepo())
	f.svc.downloadFn = func(context.Context, *whatsmeow.Client, whatsmeow.DownloadableMessage) ([]byte, error) {
		return nil, downloadErr
	}
	return f
}

func (f *historyMediaFixture) sync(recoverMedia ExpiredMediaRecoverer) error {
	opts := SyncOptions{IncludeMedia: true, RecoverExpiredMedia: recoverMedia}
	return f.svc.syncMessage(context.Background(), 42, f.msg, &whatsmeow.Client{}, opts, false)
}

// TestSyncMessageAttachesExpiredMediaRecoveredFromPhone: media that expired on
// WhatsApp's servers is attached from the phone's re-upload, and the file the
// recovery stored is removed once Chatwoot has it.
func TestSyncMessageAttachesExpiredMediaRecoveredFromPhone(t *testing.T) {
	f := newHistoryMediaFixture(t, whatsmeow.ErrMediaDownloadFailedWith404)

	err := f.sync(func(_ context.Context, _ *whatsmeow.Client, message *domainChatStorage.Message, storageLocation string, downloadErr error) (utils.ExtractedMedia, error) {
		if message != f.msg || !errors.Is(downloadErr, whatsmeow.ErrMediaDownloadFailedWith404) {
			t.Errorf("recovery got message %v and error %v", message, downloadErr)
		}
		path := filepath.Join(storageLocation, "recovered.jpg")
		return utils.ExtractedMedia{MediaPath: path}, os.WriteFile(path, []byte("phone copy"), 0o600)
	})

	if err != nil {
		t.Fatalf("syncMessage: %v", err)
	}
	if len(f.attachments) != 1 || f.attachments[0] != "phone copy" {
		t.Fatalf("attachments = %q, want the file the phone re-uploaded", f.attachments)
	}
	if strings.Contains(f.content, "[media unavailable]") {
		t.Fatalf("content = %q, want no unavailable marker", f.content)
	}
	if left, _ := os.ReadDir(f.tempDir); len(left) != 0 {
		t.Fatalf("temp dir still holds %d file(s) after the upload", len(left))
	}
}

// TestSyncMessageImportsPlaceholderWhenExpiredMediaCannotBeRecovered: a failed
// recovery must not drop the message. It is imported with the unavailable
// placeholder, like any other failed download.
func TestSyncMessageImportsPlaceholderWhenExpiredMediaCannotBeRecovered(t *testing.T) {
	f := newHistoryMediaFixture(t, whatsmeow.ErrMediaDownloadFailedWith410)

	err := f.sync(func(_ context.Context, _ *whatsmeow.Client, _ *domainChatStorage.Message, _ string, downloadErr error) (utils.ExtractedMedia, error) {
		return utils.ExtractedMedia{}, errors.Join(errors.New("phone did not answer"), downloadErr)
	})

	if err != nil {
		t.Fatalf("syncMessage: %v, want the message imported without its media", err)
	}
	if len(f.attachments) != 0 {
		t.Fatalf("attachments = %q, want none", f.attachments)
	}
	if f.content != "[image] [media unavailable]" {
		t.Fatalf("content = %q, want the unavailable placeholder", f.content)
	}
}

// TestSyncMessageDoesNotAskPhoneForOtherDownloadFailures: only 403, 404 and
// 410 mean the file is gone from the server. The phone cannot fix any other
// download error, so it must not cost a phone round trip.
func TestSyncMessageDoesNotAskPhoneForOtherDownloadFailures(t *testing.T) {
	f := newHistoryMediaFixture(t, errors.New("media connection refused"))

	err := f.sync(func(context.Context, *whatsmeow.Client, *domainChatStorage.Message, string, error) (utils.ExtractedMedia, error) {
		t.Error("recovery must only run for expired media")
		return utils.ExtractedMedia{}, nil
	})

	if err != nil {
		t.Fatalf("syncMessage: %v", err)
	}
	if f.content != "[image] [media unavailable]" {
		t.Fatalf("content = %q, want the unavailable placeholder", f.content)
	}
}

// TestSyncMessageDownloadsMediaStoredWithDirectPathOnly: a stored row can hold
// a direct path without a URL. whatsmeow downloads by direct path, so both
// REST import paths must attach such media instead of skipping the download.
func TestSyncMessageDownloadsMediaStoredWithDirectPathOnly(t *testing.T) {
	for name, requireMediaAttachment := range map[string]bool{"REST import": false, "pgimport media pre-pass": true} {
		t.Run(name, func(t *testing.T) {
			f := newHistoryMediaFixture(t, nil)
			f.msg.URL = ""
			f.msg.DirectPath = "/v/t62.7118-24/direct.enc"
			f.svc.downloadFn = func(_ context.Context, _ *whatsmeow.Client, media whatsmeow.DownloadableMessage) ([]byte, error) {
				if media.GetDirectPath() != f.msg.DirectPath {
					t.Errorf("download direct path = %q, want %q", media.GetDirectPath(), f.msg.DirectPath)
				}
				return []byte("direct"), nil
			}

			err := f.svc.syncMessageWithOptions(context.Background(), 42, f.msg, &whatsmeow.Client{}, SyncOptions{IncludeMedia: true}, false, requireMediaAttachment)

			if err != nil {
				t.Fatalf("syncMessageWithOptions: %v", err)
			}
			if len(f.attachments) != 1 || f.attachments[0] != "direct" {
				t.Fatalf("attachments = %q, want the downloaded file", f.attachments)
			}
		})
	}
}
