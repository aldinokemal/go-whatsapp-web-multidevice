package multiuser

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/stretchr/testify/require"
)

// purgeRecorder is chat storage for the wiring tests: it records each
// retention purge's cutoff.
type purgeRecorder struct {
	chatsRecorder
	mu                  sync.Mutex
	messages, scheduled []time.Time
}

func (p *purgeRecorder) PurgeMessagesBefore(cutoff time.Time) (chatstorage.PurgeResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, cutoff)
	return chatstorage.PurgeResult{}, nil
}

func (p *purgeRecorder) PurgeFinishedScheduledSendsBefore(cutoff time.Time) (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scheduled = append(p.scheduled, cutoff)
	return 0, nil
}

func (p *purgeRecorder) runs() (messages, scheduled []time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]time.Time(nil), p.messages...), append([]time.Time(nil), p.scheduled...)
}

// Wiring (phase 3): with retention off nothing is purged and history sync is
// not cut.
func TestRetentionOffStartsNoWorker(t *testing.T) {
	rt, _ := phase34Runtime(t, func(s *Settings) { s.RetentionHours = 0 })
	require.Nil(t, whatsapp.HistoryCutoffHook)
	chats := &purgeRecorder{}
	rt.Start(chats)
	require.Nil(t, rt.Retention)
	require.Never(t, func() bool {
		m, s := chats.runs()
		return len(m)+len(s) > 0
	}, 300*time.Millisecond, 20*time.Millisecond)
}

// Wiring (phase 3): with retention on, Setup cuts history sync at the window,
// Start runs the worker over chat storage, the media and send-item folders
// and the history dumps, and Close removes the cut.
func TestRetentionOnStartsTheWorkerAndSetsTheHook(t *testing.T) {
	rt, _ := phase34Runtime(t, func(s *Settings) { s.RetentionHours = 24 })

	require.NotNil(t, whatsapp.HistoryCutoffHook)
	cutoff, ok := whatsapp.HistoryCutoffHook()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(-24*time.Hour), cutoff, time.Minute)

	require.Equal(t, []string{config.PathMedia, config.PathSendItems}, rt.RetentionRoots)
	require.Equal(t, config.PathStorages, rt.HistoryDumpDir)

	// Point the sweep at temporary folders, each with one old file.
	media, sent, dumps := t.TempDir(), t.TempDir(), t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	files := []string{filepath.Join(media, "d_x", "a.jpg"), filepath.Join(sent, "b.jpg"), filepath.Join(dumps, "history-1.json")}
	for _, f := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(f), 0o755))
		require.NoError(t, os.WriteFile(f, []byte("x"), 0o600))
		require.NoError(t, os.Chtimes(f, old, old))
	}
	rt.RetentionRoots, rt.HistoryDumpDir = []string{media, sent}, dumps

	chats := &purgeRecorder{}
	rt.Start(chats)
	require.NotNil(t, rt.Retention)
	require.Equal(t, 24*time.Hour, rt.Retention.Window)
	require.Equal(t, rt.Store, rt.Retention.Accounts)
	require.Eventually(t, func() bool {
		m, s := chats.runs()
		return len(m) == 1 && len(s) == 1
	}, 5*time.Second, 20*time.Millisecond)
	m, s := chats.runs()
	require.WithinDuration(t, time.Now().Add(-24*time.Hour), m[0], time.Minute)
	require.Equal(t, m[0], s[0])
	require.Eventually(t, func() bool {
		for _, f := range files {
			if _, err := os.Stat(f); !os.IsNotExist(err) {
				return false
			}
		}
		return true
	}, 5*time.Second, 20*time.Millisecond)

	require.NoError(t, rt.Close())
	require.Nil(t, whatsapp.HistoryCutoffHook)
}

// Wiring: a Setup that fails after the hook was set leaves it unset.
func TestRetentionHookClearedWhenSetupFails(t *testing.T) {
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	saved := config.McpOAuthDBURI
	t.Cleanup(func() { config.McpOAuthDBURI = saved })
	config.McpOAuthDBURI = "file:" + filepath.Join(t.TempDir(), "missing", "dir", "oauth.db")
	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{},
		MCPEnabled: true, MCPClientHosts: []string{"claude.ai"}, RetentionHours: 24}
	rt, err := Setup(s, dm, nil, t.TempDir())
	if err == nil {
		_ = rt.Close()
		t.Skip("the OAuth store opened; this test needs it to fail")
	}
	require.Nil(t, whatsapp.HistoryCutoffHook)
}
