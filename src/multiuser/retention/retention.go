// Package retention deletes message content and media once they are older
// than the retention window (WTF_RETENTION_HOURS), for every user and device,
// the service number included. Configuration (accounts, devices, settings,
// webhooks, contacts, sessions, keys, grants and the audit log) is never
// subject to it.
package retention

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/sirupsen/logrus"
)

// Chats is the chat storage purge (IChatStorageRepository has it).
type Chats interface {
	PurgeMessagesBefore(cutoff time.Time) (chatstorage.PurgeResult, error)
}

// Accounts prunes dead rows in the accounts store (*store.Store has it).
type Accounts interface {
	PruneExpired(ctx context.Context) (store.PruneCounts, error)
}

type Worker struct {
	// Window is how long content is kept. A run with no positive window
	// deletes nothing.
	Window   time.Duration
	Chats    Chats
	Accounts Accounts
	// FileRoots are swept by modification time: statics/media and
	// statics/senditems in production.
	FileRoots []string
	// HistoryDumpDir is where upstream wrote raw history sync dumps
	// (storages). Only history-*.json directly in it is swept; "" skips it.
	HistoryDumpDir string
	// Every is the time between runs (default one hour).
	Every time.Duration
	// Now is the clock (default time.Now).
	Now func() time.Time
}

// Report counts what one run removed.
type Report struct {
	Messages, Chats int64
	Files           int
	Sessions        int64
	LoginTokens     int64
}

// RunOnce deletes everything older than now minus the window. Every step runs
// even if an earlier one failed; the errors are returned together.
func (w *Worker) RunOnce(ctx context.Context, now time.Time) (Report, error) {
	var rep Report
	if w.Window <= 0 {
		return rep, errors.New("retention: no window, nothing deleted")
	}
	if now.IsZero() {
		return rep, errors.New("retention: no clock, nothing deleted")
	}
	cutoff := now.UTC().Add(-w.Window)
	var errs []error

	if w.Chats != nil {
		res, err := w.Chats.PurgeMessagesBefore(cutoff)
		rep.Messages, rep.Chats = res.Messages, res.Chats
		if err != nil {
			errs = append(errs, fmt.Errorf("messages: %w", err))
		}
	}
	for i, root := range w.FileRoots {
		n, err := SweepDir(root, cutoff)
		rep.Files += n
		if err != nil {
			// File and directory names can carry a phone number, so only the
			// root's index and the bare cause reach the log, never a path.
			errs = append(errs, fmt.Errorf("file root %d: %s", i, pathless(err)))
		}
	}
	if w.HistoryDumpDir != "" {
		n, err := SweepHistoryDumps(w.HistoryDumpDir, cutoff)
		rep.Files += n
		if err != nil {
			errs = append(errs, fmt.Errorf("history dumps: %s", pathless(err)))
		}
	}
	if w.Accounts != nil {
		c, err := w.Accounts.PruneExpired(ctx)
		rep.Sessions, rep.LoginTokens = c.Sessions, c.LoginTokens
		if err != nil {
			errs = append(errs, fmt.Errorf("accounts: %w", err))
		}
	}
	return rep, errors.Join(errs...)
}

// Run calls RunOnce at once and then every Every until ctx is cancelled. It
// is one loop, so a run never overlaps the one before it; a slow run delays
// the next tick rather than stacking up. A failing run is logged and the
// next tick tries again.
func (w *Worker) Run(ctx context.Context) {
	every := w.Every
	if every <= 0 {
		every = time.Hour
	}
	now := w.Now
	if now == nil {
		now = time.Now
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		w.runLogged(ctx, now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (w *Worker) runLogged(ctx context.Context, now time.Time) {
	rep, err := w.RunOnce(ctx, now)
	entry := logrus.WithFields(logrus.Fields{
		"messages": rep.Messages, "chats": rep.Chats, "files": rep.Files,
		"sessions": rep.Sessions, "login_tokens": rep.LoginTokens,
	})
	if err != nil {
		entry.Errorf("[WTF] retention run finished with errors: %v", err)
		return
	}
	entry.Info("[WTF] retention run")
}

// pathless describes err without any file path in it.
func pathless(err error) string {
	var out []string
	var walk func(error)
	walk = func(e error) {
		if j, ok := e.(interface{ Unwrap() []error }); ok {
			for _, x := range j.Unwrap() {
				walk(x)
			}
			return
		}
		var pe *fs.PathError
		if errors.As(e, &pe) {
			out = append(out, pe.Op+": "+pe.Err.Error())
			return
		}
		var le *os.LinkError
		if errors.As(e, &le) {
			out = append(out, le.Op+": "+le.Err.Error())
			return
		}
		out = append(out, "sweep failed")
	}
	walk(err)
	return strings.Join(out, "; ")
}
