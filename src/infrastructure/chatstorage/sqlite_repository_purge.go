package chatstorage

// WTF: retention purge. Everything in this file is WTF's; upstream has no
// equivalent.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

var (
	// purgeBatchSize bounds how many rows one purge transaction looks at, so a
	// first run over a large backlog never holds the write lock for long.
	purgeBatchSize = 5000
	// purgeCommitted, when set (tests only), is called after each committed
	// batch.
	purgeCommitted func()
)

// setPurgeBatchForTest swaps the batch size and commit counter; the returned
// func restores them.
func setPurgeBatchForTest(size int, onCommit func()) func() {
	oldSize, oldHook := purgeBatchSize, purgeCommitted
	purgeBatchSize, purgeCommitted = size, onCommit
	return func() { purgeBatchSize, purgeCommitted = oldSize, oldHook }
}

// purgePrefilterSlack widens the index-friendly text prefilter. Timestamps are
// stored as text in whatever zone they carried (offsets run from -12h to
// +14h), so the stored wall clock of an old message can read up to 14 hours
// after the cutoff. The exact comparison happens in Go on the parsed instant;
// the prefilter only has to never exclude an old row.
const purgePrefilterSlack = 48 * time.Hour

// storedTimeLayouts are the forms a time.Time takes once a SQLite driver has
// written it (mattn: first; modernc: time.Time.String) or SQLite itself did
// (CURRENT_TIMESTAMP, UTC by definition).
var storedTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999 -0700", // modernc, after the zone name is cut off
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
}

// parseStoredTime reads a stored timestamp as an instant. ok is false when the
// text is in no known form; such a row is never deleted.
func parseStoredTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " m="); i > 0 { // monotonic clock suffix from time.String
		s = s[:i]
	}
	// time.String ends "<date> <clock> -0700 NAME". The name is free text
	// ("SAST", "+02", or anything a FixedZone was given) and the numeric
	// offset already says everything, so keep only the first three fields.
	if f := strings.Fields(s); len(f) == 4 && len(f[2]) == 5 && (f[2][0] == '+' || f[2][0] == '-') {
		s = f[0] + " " + f[1] + " " + f[2]
	}
	for _, layout := range storedTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// olderThan is the one place the retention boundary is decided: strictly
// before the cutoff, compared as instants.
func olderThan(stored string, cutoff time.Time) bool {
	t, ok := parseStoredTime(stored)
	return ok && t.Before(cutoff)
}

type purgeKey struct {
	rowid                 int64
	id, chatJID, deviceID string
	stamp                 string
}

// PurgeMessagesBefore implements retention for every device at once.
func (r *SQLiteRepository) PurgeMessagesBefore(cutoff time.Time) (domainChatStorage.PurgeResult, error) {
	var res domainChatStorage.PurgeResult
	if cutoff.IsZero() {
		return res, errors.New("purge needs a cutoff")
	}
	cutoff = cutoff.UTC()
	prefilter := cutoff.Add(purgePrefilterSlack)

	n, err := r.purgeOldMessages(cutoff, prefilter)
	res.Messages = n
	if err != nil {
		return res, err
	}
	for _, o := range orphanSweeps {
		if err := r.purgeOrphans(o, cutoff, prefilter); err != nil {
			return res, err
		}
	}
	res.Chats, err = r.purgeEmptyChats(cutoff, prefilter)
	return res, err
}

func (r *SQLiteRepository) purgeOldMessages(cutoff, prefilter time.Time) (int64, error) {
	var total int64
	var after int64
	for {
		page, err := r.scanKeys(`SELECT rowid, id, chat_jid, device_id, CAST(timestamp AS TEXT) FROM messages
			WHERE rowid > ? AND timestamp < ? ORDER BY rowid LIMIT ?`, after, prefilter, purgeBatchSize)
		if err != nil {
			return total, fmt.Errorf("purge: list messages: %w", err)
		}
		var old []purgeKey
		for _, k := range page {
			if olderThan(k.stamp, cutoff) {
				old = append(old, k)
			}
		}
		if len(old) > 0 {
			n, err := r.deleteMessageBatch(old)
			total += n
			if err != nil {
				return total, err
			}
		}
		if len(page) < purgeBatchSize {
			return total, nil
		}
		after = page[len(page)-1].rowid
	}
}

// deleteMessageBatch deletes one batch of messages and everything that hangs
// off them, in one transaction. Each message is deleted only if its timestamp
// is still the one that was judged old.
func (r *SQLiteRepository) deleteMessageBatch(keys []purgeKey) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmts := []string{
		`DELETE FROM message_reactions WHERE message_id = ? AND chat_jid = ? AND device_id = ?`,
		`DELETE FROM message_edits WHERE original_message_id = ? AND chat_jid = ? AND device_id = ?`,
		`DELETE FROM poll_definitions WHERE poll_message_id = ? AND chat_jid = ? AND device_id = ?`,
	}
	var deleted int64
	for _, k := range keys {
		res, err := tx.Exec(`DELETE FROM messages WHERE id = ? AND chat_jid = ? AND device_id = ? AND CAST(timestamp AS TEXT) = ?`,
			k.id, k.chatJID, k.deviceID, k.stamp)
		if err != nil {
			return 0, fmt.Errorf("purge: delete message: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // changed or gone since it was listed: leave it and its rows alone
		}
		deleted++
		for _, q := range stmts {
			if _, err := tx.Exec(q, k.id, k.chatJID, k.deviceID); err != nil {
				return 0, fmt.Errorf("purge: delete dependent rows: %w", err)
			}
		}
		// Chatwoot rows are keyed by (device, message id) without the chat, so
		// they go only when no message with that id is left on the device.
		for _, q := range []string{
			`DELETE FROM chatwoot_message_links WHERE device_id = ? AND wa_message_id = ?
				AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.device_id = ? AND m.id = ?)`,
			`DELETE FROM chatwoot_forward_queue WHERE device_id = ? AND wa_message_id = ?
				AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.device_id = ? AND m.id = ?)`,
		} {
			if _, err := tx.Exec(q, k.deviceID, k.id, k.deviceID, k.id); err != nil {
				return 0, fmt.Errorf("purge: delete chatwoot rows: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if purgeCommitted != nil {
		purgeCommitted()
	}
	return deleted, nil
}

// orphanSweep names a table holding message content that can outlive (or
// never have) its message, the column with its own time, and the condition
// "its message is not stored". All fragments are fixed strings.
type orphanSweep struct {
	table, timeCol, noMessage string
}

var orphanSweeps = []orphanSweep{
	{"message_reactions", "reaction_timestamp",
		`NOT EXISTS (SELECT 1 FROM messages m WHERE m.id = message_reactions.message_id AND m.chat_jid = message_reactions.chat_jid AND m.device_id = message_reactions.device_id)`},
	{"message_edits", "edited_at",
		`NOT EXISTS (SELECT 1 FROM messages m WHERE m.id = message_edits.original_message_id AND m.chat_jid = message_edits.chat_jid AND m.device_id = message_edits.device_id)`},
	{"poll_definitions", "created_at",
		`NOT EXISTS (SELECT 1 FROM messages m WHERE m.id = poll_definitions.poll_message_id AND m.chat_jid = poll_definitions.chat_jid AND m.device_id = poll_definitions.device_id)`},
	{"chatwoot_forward_queue", "created_at",
		`NOT EXISTS (SELECT 1 FROM messages m WHERE m.id = chatwoot_forward_queue.wa_message_id AND m.device_id = chatwoot_forward_queue.device_id)`},
}

// purgeOrphans deletes rows of o.table that are themselves past the cutoff and
// whose message is not stored.
func (r *SQLiteRepository) purgeOrphans(o orphanSweep, cutoff, prefilter time.Time) error {
	var after int64
	for {
		page, err := r.scanStamps(`SELECT rowid, CAST(`+o.timeCol+` AS TEXT) FROM `+o.table+
			` WHERE rowid > ? AND `+o.timeCol+` < ? AND `+o.noMessage+` ORDER BY rowid LIMIT ?`, after, prefilter, purgeBatchSize)
		if err != nil {
			return fmt.Errorf("purge: list %s: %w", o.table, err)
		}
		var old []purgeKey
		for _, k := range page {
			if olderThan(k.stamp, cutoff) {
				old = append(old, k)
			}
		}
		if len(old) > 0 {
			if err := r.deleteByRowid(`DELETE FROM `+o.table+` WHERE rowid = ? AND CAST(`+o.timeCol+` AS TEXT) = ? AND `+o.noMessage, old); err != nil {
				return fmt.Errorf("purge: delete %s: %w", o.table, err)
			}
		}
		if len(page) < purgeBatchSize {
			return nil
		}
		after = page[len(page)-1].rowid
	}
}

// purgeEmptyChats deletes chats with no messages left whose own last message
// time is past the cutoff.
func (r *SQLiteRepository) purgeEmptyChats(cutoff, prefilter time.Time) (int64, error) {
	const noMessages = `NOT EXISTS (SELECT 1 FROM messages m WHERE m.chat_jid = chats.jid AND m.device_id = chats.device_id)`
	var total, after int64
	for {
		page, err := r.scanStamps(`SELECT rowid, CAST(last_message_time AS TEXT) FROM chats
			WHERE rowid > ? AND last_message_time < ? AND `+noMessages+` ORDER BY rowid LIMIT ?`, after, prefilter, purgeBatchSize)
		if err != nil {
			return total, fmt.Errorf("purge: list chats: %w", err)
		}
		var old []purgeKey
		for _, k := range page {
			if olderThan(k.stamp, cutoff) {
				old = append(old, k)
			}
		}
		if len(old) > 0 {
			tx, err := r.db.Begin()
			if err != nil {
				return total, err
			}
			var n int64
			for _, k := range old {
				res, err := tx.Exec(`DELETE FROM chats WHERE rowid = ? AND CAST(last_message_time AS TEXT) = ? AND `+noMessages, k.rowid, k.stamp)
				if err != nil {
					_ = tx.Rollback()
					return total, fmt.Errorf("purge: delete chat: %w", err)
				}
				c, _ := res.RowsAffected()
				n += c
			}
			if err := tx.Commit(); err != nil {
				return total, err
			}
			total += n
			if purgeCommitted != nil {
				purgeCommitted()
			}
		}
		if len(page) < purgeBatchSize {
			return total, nil
		}
		after = page[len(page)-1].rowid
	}
}

func (r *SQLiteRepository) deleteByRowid(query string, keys []purgeKey) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, k := range keys {
		if _, err := tx.Exec(query, k.rowid, k.stamp); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if purgeCommitted != nil {
		purgeCommitted()
	}
	return nil
}

// scanKeys reads a page of message keys. The rows are closed before any write.
func (r *SQLiteRepository) scanKeys(query string, args ...any) ([]purgeKey, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []purgeKey
	for rows.Next() {
		var k purgeKey
		var stamp *string
		if err := rows.Scan(&k.rowid, &k.id, &k.chatJID, &k.deviceID, &stamp); err != nil {
			return nil, err
		}
		if stamp != nil {
			k.stamp = *stamp
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// scanStamps reads a page of (rowid, time text) pairs.
func (r *SQLiteRepository) scanStamps(query string, args ...any) ([]purgeKey, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []purgeKey
	for rows.Next() {
		var k purgeKey
		var stamp *string
		if err := rows.Scan(&k.rowid, &stamp); err != nil {
			return nil, err
		}
		if stamp != nil {
			k.stamp = *stamp
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
