package pgimport

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// SQL fragments for the upsertContact create-path, kept as literals so
// regexp.QuoteMeta pins the exact query the writer emits (matching the
// discipline of upsert_contact_test.go's consts).

const contactPhoneLookupSQL = `
			SELECT id
			FROM contacts
			WHERE account_id = $1 AND phone_number = $2
			LIMIT 1
		`

const insertContactSQL = `
		INSERT INTO contacts
			(account_id, name, phone_number, identifier,
			 custom_attributes, additional_attributes, created_at, updated_at)
		VALUES
			($1, $2, NULLIF($3, ''), NULLIF($4, ''),
			 $5::jsonb, '{}'::jsonb, now(), now())
		RETURNING id
	`

// expectContactCreatePath registers the three queries upsertContact issues for
// a brand-new 1:1 contact: JID miss, phone miss, INSERT RETURNING id. It
// returns nothing; callers chain it between ExpectBegin and the next stage.
func expectContactCreatePath(t *testing.T, mock sqlmock.Sqlmock, imp *Importer, jid, phone string, newID int) {
	t.Helper()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnError(noRowsError())
	mock.ExpectQuery(regexp.QuoteMeta(contactPhoneLookupSQL)).
		WithArgs(imp.accountID, phone).
		WillReturnError(noRowsError())
	mock.ExpectQuery(regexp.QuoteMeta(insertContactSQL)).
		WithArgs(imp.accountID, sqlmock.AnyArg(), phone, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(newID))
}

func TestImportChat_HappyPath(t *testing.T) {
	// End-to-end happy path for a 1:1 chat with two messages, both new:
	//   Begin
	//   upsertContact   (JID miss -> phone miss -> INSERT id=100)
	//   upsertContactInbox (SELECT miss -> INSERT id=200)
	//   findOrCreateConversation (SELECT miss -> INSERT id=300)
	//   msg1: probe miss -> INSERT
	//   msg2: probe miss -> INSERT
	//   touchConversation UPDATE
	//   Commit
	// The result counts must report 2 written, 0 skipped, 0 failed.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	const phone = "+6281234567890" // E.164 normalization of the above JID
	t1 := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	msgs := []*domainChatStorage.Message{
		{ID: "wa-1", Content: "hello", Timestamp: t1},
		{ID: "wa-2", Content: "world", Timestamp: t2},
	}

	mock.ExpectBegin()
	expectContactCreatePath(t, mock, imp, jid, phone, 100)

	// contact_inbox: SELECT miss -> INSERT ... ON CONFLICT RETURNING id.
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(100, imp.inboxID).
		WillReturnError(noRowsError())
	mock.ExpectQuery(regexp.QuoteMeta(insertContactInboxSQL)).
		WithArgs(100, imp.inboxID, jid).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(200))

	// conversation: SELECT miss -> INSERT RETURNING id, created_at pinned to t1.
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 100).
		WillReturnError(noRowsError())
	mock.ExpectQuery(regexp.QuoteMeta(insertConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, conversationStatusOpen, 100, 200, t1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(300))

	// msg1 — savepoint, probe miss, insert, release.
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-1").
		WillReturnError(noRowsError())
	mock.ExpectQuery(`INSERT INTO messages`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))

	// msg2 — same shape.
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-2").
		WillReturnError(noRowsError())
	mock.ExpectQuery(`INSERT INTO messages`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))

	// touchConversation advances last_activity_at to the latest written ts (t2).
	mock.ExpectExec(regexp.QuoteMeta(touchConversationSQL)).
		WithArgs(300, t2).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  jid,
		ChatName: "Alice",
		Messages: msgs,
	})
	if err != nil {
		t.Fatalf("ImportChat: %v", err)
	}
	if res.ContactID != 100 {
		t.Errorf("ContactID = %d, want 100", res.ContactID)
	}
	if res.ConversationID != 300 {
		t.Errorf("ConversationID = %d, want 300", res.ConversationID)
	}
	if res.MessagesWrote != 2 {
		t.Errorf("MessagesWrote = %d, want 2", res.MessagesWrote)
	}
	if res.MessagesSkipped != 0 || res.MessagesFailed != 0 {
		t.Errorf("Skipped/Failed = %d/%d, want 0/0", res.MessagesSkipped, res.MessagesFailed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_HappyPathExistingContactFastPath(t *testing.T) {
	// Variant where the contact already exists (JID fast-path hit) and the
	// contact_inbox + conversation also already exist. This exercises the
	// no-insert branches of all three upsert helpers in a single flow, with a
	// single new message written.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	ts := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	msgs := []*domainChatStorage.Message{{ID: "wa-1", Content: "hi", Timestamp: ts}}

	mock.ExpectBegin()
	// Contact fast path hits (1:1, isGroup=false => no name refresh UPDATE).
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
	// contact_inbox exists.
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(100, imp.inboxID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(200))
	// conversation exists.
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(300, conversationStatusOpen))
	// single message written.
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-1").
		WillReturnError(noRowsError())
	mock.ExpectQuery(`INSERT INTO messages`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(touchConversationSQL)).
		WithArgs(300, ts).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  jid,
		ChatName: "Alice",
		Messages: msgs,
	})
	if err != nil {
		t.Fatalf("ImportChat: %v", err)
	}
	if res.ContactID != 100 || res.ConversationID != 300 || res.MessagesWrote != 1 {
		t.Errorf("res = %+v, want contact=100 conv=300 wrote=1", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_IdempotentSkip(t *testing.T) {
	// A message whose idempotency probe finds an existing row is skipped:
	// MessagesSkipped++ and NO INSERT for it. The savepoint still wraps the
	// (no-op) probe, and because nothing was written, touchConversation is NOT
	// issued (res.MessagesWrote stays 0).
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	ts := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	msgs := []*domainChatStorage.Message{{ID: "wa-dup", Content: "hi", Timestamp: ts}}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(100, imp.inboxID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(200))
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(300, conversationStatusOpen))

	// Savepoint + probe HIT -> RELEASE, but no INSERT and no touchConversation.
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-dup").
		WillReturnRows(sqlmock.NewRows([]string{"id", "conversation_id"}).AddRow(1, 300))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectCommit()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  jid,
		ChatName: "Alice",
		Messages: msgs,
	})
	if err != nil {
		t.Fatalf("ImportChat: %v", err)
	}
	if res.MessagesWrote != 0 {
		t.Errorf("MessagesWrote = %d, want 0", res.MessagesWrote)
	}
	if res.MessagesSkipped != 1 {
		t.Errorf("MessagesSkipped = %d, want 1", res.MessagesSkipped)
	}
	if res.MessagesFailed != 0 {
		t.Errorf("MessagesFailed = %d, want 0", res.MessagesFailed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_PerMessageFailureCountedNotFatal(t *testing.T) {
	// A single message INSERT failure rolls back only that row's savepoint and
	// is counted as MessagesFailed; the loop continues. Here msg1 fails and
	// msg2 succeeds, so the chat still commits with 1 written / 1 failed.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	t1 := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Minute)
	msgs := []*domainChatStorage.Message{
		{ID: "wa-bad", Content: "boom", Timestamp: t1},
		{ID: "wa-good", Content: "ok", Timestamp: t2},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(100, imp.inboxID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(200))
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(300, conversationStatusOpen))

	// msg1 fails the INSERT -> ROLLBACK TO SAVEPOINT, not fatal.
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-bad").
		WillReturnError(noRowsError())
	mock.ExpectQuery(`INSERT INTO messages`).WillReturnError(errors.New("constraint boom"))
	mock.ExpectExec(regexp.QuoteMeta("ROLLBACK TO SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))

	// msg2 succeeds.
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-good").
		WillReturnError(noRowsError())
	mock.ExpectQuery(`INSERT INTO messages`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))

	// touchConversation uses t2 (only written msg).
	mock.ExpectExec(regexp.QuoteMeta(touchConversationSQL)).
		WithArgs(300, t2).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  jid,
		ChatName: "Alice",
		Messages: msgs,
	})
	if err != nil {
		t.Fatalf("ImportChat: %v", err)
	}
	if res.MessagesWrote != 1 || res.MessagesFailed != 1 || res.MessagesSkipped != 0 {
		t.Errorf("res = %+v, want wrote=1 failed=1 skipped=0", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_ClosedReturnsErrorNoDB(t *testing.T) {
	// A closed importer rejects the call up front. No transaction is opened, so
	// registering zero expectations proves the DB is never touched.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()
	imp.closed.Store(true)

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  "6281234567890@s.whatsapp.net",
		Messages: []*domainChatStorage.Message{{ID: "x"}},
	})
	if err == nil {
		t.Fatalf("expected error on closed importer, got nil")
	}
	if res != nil {
		t.Errorf("res = %+v, want nil", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_EmptyChatJIDReturnsErrorNoDB(t *testing.T) {
	// An empty ChatJID is rejected before BeginTx — no DB calls.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  "",
		Messages: []*domainChatStorage.Message{{ID: "x"}},
	})
	if err == nil {
		t.Fatalf("expected error on empty ChatJID, got nil")
	}
	if res != nil {
		t.Errorf("res = %+v, want nil", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_UpsertContactErrorRollsBack(t *testing.T) {
	// A non-ErrNoRows error from the very first contact lookup aborts the whole
	// import: tx is rolled back and ImportChat returns a wrapped error. No
	// further stage runs.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnError(errors.New("db down"))
	mock.ExpectRollback()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  jid,
		Messages: []*domainChatStorage.Message{{ID: "wa-1", Content: "hi"}},
	})
	if err == nil {
		t.Fatalf("expected error on upsertContact failure, got nil")
	}
	if res != nil {
		t.Errorf("res = %+v, want nil", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_FatalSavepointErrorAbortsLoopAndRollsBack(t *testing.T) {
	// A txFatalError from the message loop (here a failed SAVEPOINT) aborts the
	// whole import: ImportChat rolls back and returns a wrapped error. The
	// deliberately-NOT-incremented MessagesFailed counter is asserted via the
	// returned res being non-nil but carrying zero failed (the caller counts
	// every message as failed when ImportChat errors, so double-counting is
	// avoided).
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	msgs := []*domainChatStorage.Message{
		{ID: "wa-1", Content: "hi", Timestamp: time.Now()},
		{ID: "wa-2", Content: "ho", Timestamp: time.Now()},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(100, imp.inboxID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(200))
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(300, conversationStatusOpen))

	// First message's SAVEPOINT fails => txFatalError => loop aborts before
	// touching msg2. Only one SAVEPOINT is expected; a second would be unmet.
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  jid,
		ChatName: "Alice",
		Messages: msgs,
	})
	if err == nil {
		t.Fatalf("expected fatal error, got nil")
	}
	// res is returned (non-nil) so the caller can read partial progress, but the
	// failed counter is intentionally left at 0 to avoid double-counting.
	if res == nil {
		t.Fatalf("res = nil, want non-nil partial result")
	}
	if res.MessagesFailed != 0 || res.MessagesWrote != 0 {
		t.Errorf("res = %+v, want wrote=0 failed=0 on fatal abort", res)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_CanceledContextFailsAtBeginTx(t *testing.T) {
	// An already-canceled context fails at BeginTx — database/sql checks ctx
	// before reaching the driver — so ImportChat returns the wrapped "begin tx"
	// error with res == nil and never registers any mocked query. This codifies
	// that cancellation before the transaction opens is a hard failure, not a
	// partial import.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := imp.ImportChat(ctx, ImportChatRequest{
		ChatJID:  "6281234567890@s.whatsapp.net",
		Messages: []*domainChatStorage.Message{{ID: "wa-1", Content: "hi", Timestamp: time.Now()}},
	})
	if err == nil {
		t.Fatalf("expected error on canceled context, got nil")
	}
	if res != nil {
		t.Errorf("res = %+v, want nil when BeginTx fails", res)
	}
	// No mocked expectations were registered, proving no query ran past begin.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestImportChat_GroupSkipsTouchWhenAllZeroTimestampsFallBackToNow(t *testing.T) {
	// A group chat where the only written message carries a zero Timestamp:
	// lastActivity stays zero through the loop, so ImportChat falls back to
	// time.Now() for the touchConversation argument (guarding against the
	// thread landing at year 0001 in Chatwoot). We assert the touch arg is
	// now-ish rather than the zero time.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const groupJID = "120363123456789@g.us"
	// Zero timestamp on the single message; group incoming so it gets a sender
	// prefix and is therefore non-empty content.
	msgs := []*domainChatStorage.Message{
		{ID: "wa-1", Content: "hey", Sender: "628@s.whatsapp.net"},
	}

	mock.ExpectBegin()
	// Group contact fast-path hit; name non-empty triggers the refresh UPDATE.
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, groupJID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(100))
	mock.ExpectExec(regexp.QuoteMeta(refreshContactNameSQL)).
		WithArgs("Team", 100, imp.accountID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(100, imp.inboxID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(200))
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(300, conversationStatusOpen))

	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-1").
		WillReturnError(noRowsError())
	mock.ExpectQuery(`INSERT INTO messages`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).WillReturnResult(sqlmock.NewResult(0, 0))

	before := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(touchConversationSQL)).
		WithArgs(300, nowishArg{notBefore: before}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res, err := imp.ImportChat(context.Background(), ImportChatRequest{
		ChatJID:  groupJID,
		ChatName: "Team",
		Messages: msgs,
	})
	if err != nil {
		t.Fatalf("ImportChat: %v", err)
	}
	if res.MessagesWrote != 1 {
		t.Errorf("MessagesWrote = %d, want 1", res.MessagesWrote)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}
