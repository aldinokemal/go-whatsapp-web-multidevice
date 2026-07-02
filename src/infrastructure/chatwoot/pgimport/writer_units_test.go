package pgimport

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// SQL fragments below mirror the exact text the writer sends so that
// regexp.QuoteMeta turns each into a literal matcher. Pinning the literal
// (rather than a loose substring) means a meaningful query edit surfaces as
// a test failure instead of silently passing — the same discipline the
// existing upsert_contact_test.go applies.

const insertContactInboxSQL = `
		INSERT INTO contact_inboxes
			(contact_id, inbox_id, source_id, hmac_verified,
			 pubsub_token, created_at, updated_at)
		VALUES
			($1, $2, $3, false,
			 replace(gen_random_uuid()::text, '-', ''), now(), now())
		ON CONFLICT (inbox_id, source_id) DO UPDATE
			SET updated_at = EXCLUDED.updated_at
		RETURNING id
	`

const selectContactInboxSQL = `
		SELECT id
		FROM contact_inboxes
		WHERE contact_id = $1 AND inbox_id = $2
		LIMIT 1
	`

const selectConversationSQL = `
		SELECT id, status
		FROM conversations
		WHERE account_id = $1
		  AND inbox_id = $2
		  AND contact_id = $3
		ORDER BY id DESC
		LIMIT 1
	`

const insertConversationSQL = `
		INSERT INTO conversations
			(account_id, inbox_id, status, contact_id, contact_inbox_id,
			 uuid, additional_attributes, custom_attributes,
			 last_activity_at, waiting_since, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5,
			 gen_random_uuid(),
			 '{}'::jsonb, '{}'::jsonb,
			 $6, $6, $6, now())
		RETURNING id
	`

const idempotencyProbeSQL = `
		SELECT id, conversation_id FROM messages
		WHERE inbox_id = $1 AND source_id = $2
		LIMIT 1
	`

const touchConversationSQL = `
		UPDATE conversations
		SET last_activity_at = GREATEST(COALESCE(last_activity_at, $2), $2),
		    updated_at = now()
		WHERE id = $1
	`

// ---------------------------------------------------------------------------
// buildContent — cases not already covered by identity_test.go
// ---------------------------------------------------------------------------

func TestBuildContent_OutgoingMediaPlaceholder(t *testing.T) {
	// Outgoing (IsFromMe) media with an empty body falls through to the media
	// placeholder when the feature flag is on. The group-prefix branch is
	// skipped because IsFromMe is true, so no sender label is prepended.
	prev := config.ChatwootImportPlaceholderMediaMessage
	defer func() { config.ChatwootImportPlaceholderMediaMessage = prev }()
	config.ChatwootImportPlaceholderMediaMessage = true

	msg := &domainChatStorage.Message{MediaType: "video", IsFromMe: true, Sender: "628@s.whatsapp.net"}
	if got := buildContent(msg, true); got != "[video]" {
		t.Errorf("buildContent outgoing media = %q, want %q", got, "[video]")
	}
}

func TestBuildContent_GroupOutgoingNoPrefix(t *testing.T) {
	// In a group, our own outgoing messages (IsFromMe) must NOT be prefixed
	// with a sender label — the prefix is only for distinguishing *other*
	// participants. The body is returned verbatim.
	msg := &domainChatStorage.Message{Content: "my reply", IsFromMe: true, Sender: "628@s.whatsapp.net"}
	if got := buildContent(msg, true); got != "my reply" {
		t.Errorf("buildContent group outgoing = %q, want %q", got, "my reply")
	}
}

func TestBuildContent_WhitespaceTrimmedThenMediaPlaceholder(t *testing.T) {
	// A body that is only whitespace trims to "" so the media-placeholder
	// branch takes over. This guards the TrimSpace step feeding the empty
	// check that gates the placeholder.
	prev := config.ChatwootImportPlaceholderMediaMessage
	defer func() { config.ChatwootImportPlaceholderMediaMessage = prev }()
	config.ChatwootImportPlaceholderMediaMessage = true

	msg := &domainChatStorage.Message{Content: "   \t  ", MediaType: "audio"}
	if got := buildContent(msg, false); got != "[audio]" {
		t.Errorf("buildContent whitespace+media = %q, want %q", got, "[audio]")
	}
}

func TestBuildContent_GroupEmptyBodyPlaceholderOffYieldsLabelOnly(t *testing.T) {
	// Group incoming message, no body, media placeholder disabled: body stays
	// empty after trimming so the group branch returns "<label>:" (the colon
	// form), proving the label-only path is reachable.
	prev := config.ChatwootImportPlaceholderMediaMessage
	defer func() { config.ChatwootImportPlaceholderMediaMessage = prev }()
	config.ChatwootImportPlaceholderMediaMessage = false

	msg := &domainChatStorage.Message{MediaType: "image", Sender: "628111@s.whatsapp.net"}
	if got := buildContent(msg, true); got != "628111:" {
		t.Errorf("buildContent group empty body = %q, want %q", got, "628111:")
	}
}

// ---------------------------------------------------------------------------
// insertMessage — error / empty-content / idempotency branches.
// Sender resolution itself is covered in writer_test.go; these exercise the
// early-return paths that the sender-resolution table does not.
// ---------------------------------------------------------------------------

func TestInsertMessage_EmptyIDReturnsErrorNoSQL(t *testing.T) {
	// An empty message ID is rejected before any SQL is issued — registering
	// no expectations proves the function never touches the connection.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	mock.ExpectBegin()
	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	_, wrote, err := imp.insertMessage(context.Background(), tx, 10, 20,
		&domainChatStorage.Message{ID: ""}, false)
	if wrote {
		t.Errorf("wrote = true, want false on empty ID")
	}
	if err == nil || err.Error() != "empty message ID" {
		t.Errorf("err = %v, want \"empty message ID\"", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessage_IdempotencyHitReturnsFalseNoInsert(t *testing.T) {
	// When the idempotency probe finds an existing row, insertMessage returns
	// (false, nil) and must NOT issue an INSERT — the row already exists.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-dup").
		WillReturnRows(sqlmock.NewRows([]string{"id", "conversation_id"}).AddRow(1, 300))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessage(context.Background(), tx, 10, 20,
		&domainChatStorage.Message{ID: "wa-dup", Content: "hi"}, false)
	if err != nil {
		t.Fatalf("insertMessage: %v", err)
	}
	if wrote {
		t.Errorf("wrote = true, want false on idempotency hit")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessage_EmptyContentSkipsAfterProbeMiss(t *testing.T) {
	// Probe misses (ErrNoRows) but the computed content is empty (no body, no
	// media placeholder): insertMessage returns (false, nil) WITHOUT an INSERT
	// so blank rows never pollute Chatwoot's UI.
	prev := config.ChatwootImportPlaceholderMediaMessage
	defer func() { config.ChatwootImportPlaceholderMediaMessage = prev }()
	config.ChatwootImportPlaceholderMediaMessage = false

	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-empty").
		WillReturnError(sql.ErrNoRows)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	// Content empty and MediaType set but placeholder OFF => content == "".
	_, wrote, err := imp.insertMessage(context.Background(), tx, 10, 20,
		&domainChatStorage.Message{ID: "wa-empty", MediaType: "image"}, false)
	if err != nil {
		t.Fatalf("insertMessage: %v", err)
	}
	if wrote {
		t.Errorf("wrote = true, want false on empty content")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessage_ProbeQueryErrorPropagates(t *testing.T) {
	// A non-ErrNoRows probe error is surfaced to the caller verbatim (not
	// swallowed as a skip), so the savepoint wrapper can roll back the row.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	probeErr := errors.New("probe boom")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-err").
		WillReturnError(probeErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessage(context.Background(), tx, 10, 20,
		&domainChatStorage.Message{ID: "wa-err", Content: "hi"}, false)
	if wrote {
		t.Errorf("wrote = true, want false on probe error")
	}
	if !errors.Is(err, probeErr) {
		t.Errorf("err = %v, want probe boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessage_ReturnsChatwootLinkForInsertedMessage(t *testing.T) {
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	now := time.Date(2026, time.June, 6, 11, 0, 0, 0, time.UTC)
	msg := &domainChatStorage.Message{
		ID:        "wa-linked",
		DeviceID:  "device-a@s.whatsapp.net",
		ChatJID:   "628123456789@s.whatsapp.net",
		Content:   "linked message",
		Timestamp: now,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-linked").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO messages`).
		WithArgs(
			"linked message",
			imp.accountID,
			imp.inboxID,
			10,
			messageTypeIncoming,
			now,
			messageStatusDelivered,
			"WAID:wa-linked",
			contentTypeText,
			"Contact",
			int64(20),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(303))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	link, wrote, err := imp.insertMessage(context.Background(), tx, 10, 20, msg, false)
	if err != nil {
		t.Fatalf("insertMessage: %v", err)
	}
	if !wrote {
		t.Fatal("wrote = false, want true")
	}
	if link == nil {
		t.Fatal("expected chatwoot message link")
	}
	if link.ChatwootMessageID != 303 || link.ChatwootConversationID != 10 || link.SourceID != "WAID:wa-linked" {
		t.Fatalf("unexpected link: %+v", link)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessage_GroupIncomingContentGetsSenderPrefix(t *testing.T) {
	// Group incoming message: the INSERT's content arg carries the
	// "<label>: <body>" prefix produced by buildContent. Pinning the content
	// arg proves the group-prefix branch feeds the persisted row, not just the
	// unit-tested buildContent helper.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	ts := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	msg := &domainChatStorage.Message{
		ID:        "wa-grp",
		Content:   "morning",
		Timestamp: ts,
		Sender:    "628999@s.whatsapp.net",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:wa-grp").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO messages`).
		WithArgs(
			"628999: morning", // content carries the group sender prefix
			imp.accountID,
			imp.inboxID,
			55,               // conversation_id
			0,                // message_type (incoming)
			ts,               // created_at/updated_at
			1,                // status (delivered)
			"WAID:wa-grp",    // source_id
			0,                // content_type
			"Contact",        // sender_type (incoming -> Contact)
			int64(77),        // sender_id (contactID)
			sqlmock.AnyArg(), // additional_attributes
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessage(context.Background(), tx, 55, 77, msg, true)
	if err != nil {
		t.Fatalf("insertMessage: %v", err)
	}
	if !wrote {
		t.Errorf("wrote = false, want true")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// insertMessageSavepoint — SAVEPOINT / RELEASE / ROLLBACK orchestration.
// ---------------------------------------------------------------------------

func TestInsertMessageSavepoint_Success(t *testing.T) {
	// Happy path: SAVEPOINT -> probe miss -> INSERT -> RELEASE SAVEPOINT.
	// wrote=true and no error.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	ts := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)
	msg := &domainChatStorage.Message{ID: "sp-ok", Content: "hi", Timestamp: ts}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:sp-ok").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO messages`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessageSavepoint(context.Background(), tx, 10, 20, msg, false)
	if err != nil {
		t.Fatalf("insertMessageSavepoint: %v", err)
	}
	if !wrote {
		t.Errorf("wrote = false, want true")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessageSavepoint_InsertErrorRollsBackToSavepoint(t *testing.T) {
	// A per-row INSERT error is NOT fatal: the function rolls back to the
	// savepoint and returns the underlying error (un-wrapped, not txFatalError)
	// so the caller counts a single failed message and keeps going.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	insertErr := errors.New("insert boom")
	msg := &domainChatStorage.Message{ID: "sp-fail", Content: "hi"}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:sp-fail").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO messages`).
		WillReturnError(insertErr)
	mock.ExpectExec(regexp.QuoteMeta("ROLLBACK TO SAVEPOINT cw_msg")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessageSavepoint(context.Background(), tx, 10, 20, msg, false)
	if wrote {
		t.Errorf("wrote = true, want false on insert error")
	}
	if !errors.Is(err, insertErr) {
		t.Errorf("err = %v, want insert boom", err)
	}
	// Must NOT be a txFatalError — a single bad row is recoverable.
	var fatal txFatalError
	if errors.As(err, &fatal) {
		t.Errorf("err classified as txFatalError; want recoverable error")
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessageSavepoint_SavepointExecErrorIsFatal(t *testing.T) {
	// If creating the SAVEPOINT itself fails, the whole tx is broken: the
	// function returns a txFatalError so ImportChat aborts the message loop
	// instead of churning every remaining row.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	spErr := errors.New("savepoint boom")
	msg := &domainChatStorage.Message{ID: "sp-conn", Content: "hi"}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).
		WillReturnError(spErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessageSavepoint(context.Background(), tx, 10, 20, msg, false)
	if wrote {
		t.Errorf("wrote = true, want false on savepoint error")
	}
	var fatal txFatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %v, want txFatalError", err)
	}
	if !errors.Is(err, spErr) {
		t.Errorf("txFatalError did not wrap underlying savepoint error: %v", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessageSavepoint_ReleaseErrorIsFatal(t *testing.T) {
	// RELEASE failing on an otherwise-successful write means the connection is
	// wedged. The function returns txFatalError but preserves wrote=true (the
	// row did land) so callers can distinguish "written then connection died".
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	relErr := errors.New("release boom")
	msg := &domainChatStorage.Message{ID: "sp-rel", Content: "hi"}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:sp-rel").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO messages`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectExec(regexp.QuoteMeta("RELEASE SAVEPOINT cw_msg")).
		WillReturnError(relErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessageSavepoint(context.Background(), tx, 10, 20, msg, false)
	if !wrote {
		t.Errorf("wrote = false, want true (the row was written before RELEASE failed)")
	}
	var fatal txFatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %v, want txFatalError", err)
	}
	if !errors.Is(err, relErr) {
		t.Errorf("txFatalError did not wrap underlying release error: %v", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestInsertMessageSavepoint_RollbackErrorBecomesFatal(t *testing.T) {
	// When the per-row INSERT fails AND the ROLLBACK TO SAVEPOINT also fails,
	// the connection is unrecoverable: the recoverable insert error is
	// upgraded to txFatalError so the loop aborts rather than spinning.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	insertErr := errors.New("insert boom")
	rbErr := errors.New("rollback boom")
	msg := &domainChatStorage.Message{ID: "sp-rb", Content: "hi"}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT cw_msg")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(idempotencyProbeSQL)).
		WithArgs(imp.inboxID, "WAID:sp-rb").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`INSERT INTO messages`).
		WillReturnError(insertErr)
	mock.ExpectExec(regexp.QuoteMeta("ROLLBACK TO SAVEPOINT cw_msg")).
		WillReturnError(rbErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, wrote, err := imp.insertMessageSavepoint(context.Background(), tx, 10, 20, msg, false)
	if wrote {
		t.Errorf("wrote = true, want false")
	}
	var fatal txFatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("err = %v, want txFatalError when rollback fails", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// upsertContact — phone fallback branch (JID miss -> phone hit -> attach attr)
// ---------------------------------------------------------------------------

const attachJIDAttributeSQL = `
				UPDATE contacts
				SET custom_attributes = COALESCE(custom_attributes, '{}'::jsonb)
				                        || jsonb_build_object('gowa_whatsapp_jid', $1::text),
				    updated_at = now()
				WHERE id = $2
			`

func TestUpsertContact_PhoneFallbackFoundAttachesJID(t *testing.T) {
	// 1:1 chat where the JID lookup misses but a contact with the same phone
	// number already exists (e.g. created by the live REST path without our
	// custom attribute). We reuse that row and attach gowa_whatsapp_jid so the
	// next import resolves it on the fast path — no duplicate contact created.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	const phone = "+6281234567890"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT id
			FROM contacts
			WHERE account_id = $1 AND phone_number = $2
			LIMIT 1
		`)).
		WithArgs(imp.accountID, phone).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(555))
	mock.ExpectExec(regexp.QuoteMeta(attachJIDAttributeSQL)).
		WithArgs(jid, 555).
		WillReturnResult(sqlmock.NewResult(0, 1))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.upsertContact(context.Background(), tx, jid, "Alice", false)
	if err != nil {
		t.Fatalf("upsertContact: %v", err)
	}
	if id != 555 {
		t.Errorf("id = %d, want 555 (reused phone-matched contact)", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContact_PhoneLookupErrorPropagates(t *testing.T) {
	// A non-ErrNoRows error from the phone fallback lookup is surfaced to the
	// caller (which rolls back the whole chat tx) — it must not silently fall
	// through to an INSERT and risk a duplicate.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	const phone = "+6281234567890"
	lookupErr := errors.New("phone lookup boom")

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT id
			FROM contacts
			WHERE account_id = $1 AND phone_number = $2
			LIMIT 1
		`)).
		WithArgs(imp.accountID, phone).
		WillReturnError(lookupErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = imp.upsertContact(context.Background(), tx, jid, "Alice", false)
	if !errors.Is(err, lookupErr) {
		t.Errorf("err = %v, want phone lookup boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContact_InsertContactErrorPropagates(t *testing.T) {
	// Brand-new contact path where the INSERT ... RETURNING id fails: the error
	// is wrapped ("insert contact: ...") and returned so the chat tx rolls back.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	const phone = "+6281234567890"
	insErr := errors.New("insert contact boom")

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`
			SELECT id
			FROM contacts
			WHERE account_id = $1 AND phone_number = $2
			LIMIT 1
		`)).
		WithArgs(imp.accountID, phone).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO contacts
			(account_id, name, phone_number, identifier,
			 custom_attributes, additional_attributes, created_at, updated_at)
		VALUES
			($1, $2, NULLIF($3, ''), NULLIF($4, ''),
			 $5::jsonb, '{}'::jsonb, now(), now())
		RETURNING id
	`)).
		WithArgs(imp.accountID, sqlmock.AnyArg(), phone, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(insErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = imp.upsertContact(context.Background(), tx, jid, "Alice", false)
	if !errors.Is(err, insErr) {
		t.Errorf("err = %v, want wrapped insert contact boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContact_GroupCreatesNewWithJIDIdentifier(t *testing.T) {
	// A brand-new group contact has no phone number, so the phone fallback is
	// skipped entirely (phone == "") and the INSERT runs with the JID as
	// identifier. Asserts the create path for groups bypasses the phone branch.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const groupJID = "120363123456789@g.us"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, groupJID).
		WillReturnError(sql.ErrNoRows)
	// No phone lookup expected for a group (phone == ""). INSERT runs with an
	// empty phone_number arg (NULLIF turns "" into NULL) and the JID identifier.
	mock.ExpectQuery(regexp.QuoteMeta(`
		INSERT INTO contacts
			(account_id, name, phone_number, identifier,
			 custom_attributes, additional_attributes, created_at, updated_at)
		VALUES
			($1, $2, NULLIF($3, ''), NULLIF($4, ''),
			 $5::jsonb, '{}'::jsonb, now(), now())
	`)).
		WithArgs(imp.accountID, "Team", "", groupJID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(666))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.upsertContact(context.Background(), tx, groupJID, "Team", true)
	if err != nil {
		t.Fatalf("upsertContact: %v", err)
	}
	if id != 666 {
		t.Errorf("id = %d, want 666", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContact_JIDLookupErrorPropagates(t *testing.T) {
	// A non-ErrNoRows error from the very first JID lookup short-circuits with
	// the raw error (no phone fallback, no INSERT attempted).
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	jidErr := errors.New("jid lookup boom")

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnError(jidErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = imp.upsertContact(context.Background(), tx, jid, "Alice", false)
	if !errors.Is(err, jidErr) {
		t.Errorf("err = %v, want jid lookup boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// upsertContactInbox
// ---------------------------------------------------------------------------

func TestUpsertContactInbox_ExistingReturnsIDNoInsert(t *testing.T) {
	// The SELECT finds an existing contact_inbox row, so the function returns
	// its id WITHOUT issuing the INSERT ... ON CONFLICT.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(500, imp.inboxID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(900))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.upsertContactInbox(context.Background(), tx, 500, "628@s.whatsapp.net")
	if err != nil {
		t.Fatalf("upsertContactInbox: %v", err)
	}
	if id != 900 {
		t.Errorf("id = %d, want 900", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContactInbox_MissingInsertsReturnsID(t *testing.T) {
	// SELECT misses (ErrNoRows) => INSERT ... ON CONFLICT (inbox_id, source_id)
	// runs with the contact id, inbox id, and JID as source_id, returning the
	// new id.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "628@s.whatsapp.net"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(500, imp.inboxID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(insertContactInboxSQL)).
		WithArgs(500, imp.inboxID, jid).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(901))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.upsertContactInbox(context.Background(), tx, 500, jid)
	if err != nil {
		t.Fatalf("upsertContactInbox: %v", err)
	}
	if id != 901 {
		t.Errorf("id = %d, want 901", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContactInbox_SelectErrorPropagates(t *testing.T) {
	// A non-ErrNoRows SELECT error short-circuits before the INSERT, surfacing
	// the underlying error to the caller (which rolls back the whole chat tx).
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	selErr := errors.New("select boom")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(500, imp.inboxID).
		WillReturnError(selErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = imp.upsertContactInbox(context.Background(), tx, 500, "628@s.whatsapp.net")
	if !errors.Is(err, selErr) {
		t.Errorf("err = %v, want select boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContactInbox_InsertErrorPropagates(t *testing.T) {
	// SELECT misses, then the INSERT ... ON CONFLICT RETURNING id fails: the
	// error is wrapped ("insert contact_inbox: ...") and returned.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "628@s.whatsapp.net"
	insErr := errors.New("inbox insert boom")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectContactInboxSQL)).
		WithArgs(500, imp.inboxID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(insertContactInboxSQL)).
		WithArgs(500, imp.inboxID, jid).
		WillReturnError(insErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = imp.upsertContactInbox(context.Background(), tx, 500, jid)
	if !errors.Is(err, insErr) {
		t.Errorf("err = %v, want wrapped inbox insert boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// findOrCreateConversation
// ---------------------------------------------------------------------------

func TestFindOrCreateConversation_ExistingReturnsIDNoInsert(t *testing.T) {
	// An existing conversation is reused (matched regardless of status) so a
	// resolved-then-reimported chat does not spawn a duplicate row.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(42, conversationStatusOpen))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.findOrCreateConversation(context.Background(), tx, 500, 600, nil)
	if err != nil {
		t.Fatalf("findOrCreateConversation: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_CreatesWithFirstMessageTimestamp(t *testing.T) {
	// On a miss, the new conversation's created_at (and waiting_since /
	// last_activity_at, all the same $6 placeholder) is pinned to msgs[0]'s
	// timestamp so Chatwoot's UI sorts the thread at the real first-message
	// time, not import time.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	first := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	msgs := []*domainChatStorage.Message{
		{ID: "m1", Timestamp: first},
		{ID: "m2", Timestamp: first.Add(time.Hour)},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(insertConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, conversationStatusOpen, 500, 600, first).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(43))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.findOrCreateConversation(context.Background(), tx, 500, 600, msgs)
	if err != nil {
		t.Fatalf("findOrCreateConversation: %v", err)
	}
	if id != 43 {
		t.Errorf("id = %d, want 43", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_EmptyMsgsUsesNowish(t *testing.T) {
	// With no messages (and likewise a zero-time first message), created_at
	// falls back to time.Now(). We can't pin an exact value, so we assert the
	// arg is a non-zero time close to now via a custom matcher.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	before := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(insertConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, conversationStatusOpen, 500, 600,
			nowishArg{notBefore: before}).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(44))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.findOrCreateConversation(context.Background(), tx, 500, 600, nil)
	if err != nil {
		t.Fatalf("findOrCreateConversation: %v", err)
	}
	if id != 44 {
		t.Errorf("id = %d, want 44", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_ZeroTimeFirstMsgUsesNowish(t *testing.T) {
	// A first message carrying a zero Timestamp also triggers the now() branch
	// (the guard is `!msgs[0].Timestamp.IsZero()`), so created_at is now-ish
	// rather than year 0001.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	before := time.Now()
	msgs := []*domainChatStorage.Message{{ID: "m1"}} // zero Timestamp

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(insertConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, conversationStatusOpen, 500, 600,
			nowishArg{notBefore: before}).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(45))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.findOrCreateConversation(context.Background(), tx, 500, 600, msgs)
	if err != nil {
		t.Fatalf("findOrCreateConversation: %v", err)
	}
	if id != 45 {
		t.Errorf("id = %d, want 45", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_SelectErrorPropagates(t *testing.T) {
	// Non-ErrNoRows SELECT error short-circuits before the INSERT.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	selErr := errors.New("conv select boom")
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnError(selErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = imp.findOrCreateConversation(context.Background(), tx, 500, 600, nil)
	if !errors.Is(err, selErr) {
		t.Errorf("err = %v, want conv select boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_InsertErrorPropagates(t *testing.T) {
	// SELECT misses, then the INSERT ... RETURNING id fails: the error is
	// wrapped ("insert conversation: ...") and returned to abort the chat tx.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	first := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	msgs := []*domainChatStorage.Message{{ID: "m1", Timestamp: first}}
	insErr := errors.New("conv insert boom")

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(insertConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, conversationStatusOpen, 500, 600, first).
		WillReturnError(insErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, err = imp.findOrCreateConversation(context.Background(), tx, 500, 600, msgs)
	if !errors.Is(err, insErr) {
		t.Errorf("err = %v, want wrapped conv insert boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_CreatesPendingWhenConfigured(t *testing.T) {
	// With CHATWOOT_CONVERSATION_PENDING=true, a newly created conversation must
	// land in `pending` (status 2), mirroring the REST path, instead of `open`.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	prevPending := config.ChatwootConversationPending
	defer func() { config.ChatwootConversationPending = prevPending }()
	config.ChatwootConversationPending = true

	first := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	msgs := []*domainChatStorage.Message{{ID: "m1", Timestamp: first}}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(insertConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, conversationStatusPending, 500, 600, first).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(43))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.findOrCreateConversation(context.Background(), tx, 500, 600, msgs)
	if err != nil {
		t.Fatalf("findOrCreateConversation: %v", err)
	}
	if id != 43 {
		t.Errorf("id = %d, want 43", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_ReopensResolvedReusedConversation(t *testing.T) {
	// With CHATWOOT_REOPEN_CONVERSATION=true (default), reusing a *resolved*
	// conversation flips it back to the new-status via an UPDATE so the returning
	// customer's thread resurfaces in the agent queue — matching the REST path.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	prevReopen := config.ChatwootReopenConversation
	prevPending := config.ChatwootConversationPending
	defer func() {
		config.ChatwootReopenConversation = prevReopen
		config.ChatwootConversationPending = prevPending
	}()
	config.ChatwootReopenConversation = true
	config.ChatwootConversationPending = false

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(42, conversationStatusResolved))
	mock.ExpectExec("UPDATE conversations").
		WithArgs(conversationStatusOpen, 42, imp.accountID, conversationStatusResolved).
		WillReturnResult(sqlmock.NewResult(0, 1))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.findOrCreateConversation(context.Background(), tx, 500, 600, nil)
	if err != nil {
		t.Fatalf("findOrCreateConversation: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestFindOrCreateConversation_NoReopenWhenDisabled(t *testing.T) {
	// With CHATWOOT_REOPEN_CONVERSATION=false, a reused resolved conversation is
	// returned as-is with no status UPDATE (no extra query is expected).
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = false

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(selectConversationSQL)).
		WithArgs(imp.accountID, imp.inboxID, 500).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(42, conversationStatusResolved))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.findOrCreateConversation(context.Background(), tx, 500, 600, nil)
	if err != nil {
		t.Fatalf("findOrCreateConversation: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// nowishArg matches a driver time.Time argument that is at or after notBefore
// and no further than a few seconds into the future — i.e. a value produced by
// a time.Now() call during the test. It lets us assert the now() fallback
// branch without pinning an exact instant.
type nowishArg struct {
	notBefore time.Time
}

func (n nowishArg) Match(v driver.Value) bool {
	got, ok := v.(time.Time)
	if !ok {
		return false
	}
	if got.IsZero() {
		return false
	}
	return !got.Before(n.notBefore) && got.Before(n.notBefore.Add(10*time.Second))
}

// ---------------------------------------------------------------------------
// touchConversation
// ---------------------------------------------------------------------------

func TestTouchConversation_IssuesGreatestUpdate(t *testing.T) {
	// touchConversation runs the GREATEST(...) UPDATE with the conversation id
	// ($1) and the supplied activity time ($2). GREATEST keeps last_activity_at
	// monotonic so a later import never moves the thread backwards.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(touchConversationSQL)).
		WithArgs(777, at).
		WillReturnResult(sqlmock.NewResult(0, 1))

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := imp.touchConversation(context.Background(), tx, 777, at); err != nil {
		t.Fatalf("touchConversation: %v", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestTouchConversation_ErrorPropagates(t *testing.T) {
	// touchConversation surfaces the UPDATE error to its caller; ImportChat
	// treats it as a transaction failure and rolls back the chat import.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	updErr := errors.New("touch boom")
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(touchConversationSQL)).
		WithArgs(777, at).
		WillReturnError(updErr)

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := imp.touchConversation(context.Background(), tx, 777, at); !errors.Is(err, updErr) {
		t.Errorf("err = %v, want touch boom", err)
	}
	_ = tx.Rollback()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestClose_IdempotentSetsClosed(t *testing.T) {
	// Close uses sync.Once: the first call closes the pool and flips the closed
	// flag; the second is a no-op (ExpectClose is registered once, so a second
	// db.Close would trip an unexpected-call failure). The flag must read true
	// after the first call.
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	mock.ExpectClose()

	if err := imp.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if !imp.closed.Load() {
		t.Errorf("closed flag = false after Close, want true")
	}
	// Second call must not touch the DB again (sync.Once guards it).
	if err := imp.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}
