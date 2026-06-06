package pgimport

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// insertMessageTestCase exercises insertMessage end-to-end against sqlmock,
// asserting on the sender_type/sender_id arguments passed to INSERT. The
// regex matchers are intentionally lenient on whitespace but pinned on the
// table and column structure so a meaningful schema change in the writer
// would surface as a test failure rather than silently breaking attribution.
type insertMessageTestCase struct {
	name           string
	agentUserType  string
	agentUserID    int64
	msg            *domainChatStorage.Message
	wantSenderType any // string or nil — nil means SQL NULL
	wantSenderID   any // int64 or nil — nil means SQL NULL
}

func TestInsertMessage_SenderResolution(t *testing.T) {
	const (
		convID    = 100
		contactID = 200
	)

	now := time.Date(2026, 5, 9, 10, 0, 0, 0, time.UTC)

	cases := []insertMessageTestCase{
		{
			name:           "incoming message stamps Contact / contactID regardless of agent state",
			agentUserType:  "User",
			agentUserID:    42,
			msg:            &domainChatStorage.Message{ID: "wa-1", Content: "hi", Timestamp: now, IsFromMe: false, Sender: "628111@s.whatsapp.net"},
			wantSenderType: "Contact",
			wantSenderID:   int64(contactID),
		},
		{
			name:           "outgoing message with resolved User agent stamps that agent",
			agentUserType:  "User",
			agentUserID:    42,
			msg:            &domainChatStorage.Message{ID: "wa-2", Content: "reply", Timestamp: now, IsFromMe: true},
			wantSenderType: "User",
			wantSenderID:   int64(42),
		},
		{
			name:           "outgoing message with resolved AgentBot stamps that bot",
			agentUserType:  "AgentBot",
			agentUserID:    7,
			msg:            &domainChatStorage.Message{ID: "wa-3", Content: "automated", Timestamp: now, IsFromMe: true},
			wantSenderType: "AgentBot",
			wantSenderID:   int64(7),
		},
		{
			name:           "outgoing message without agent leaves sender NULL",
			agentUserType:  "",
			agentUserID:    0,
			msg:            &domainChatStorage.Message{ID: "wa-4", Content: "lonely", Timestamp: now, IsFromMe: true},
			wantSenderType: nil,
			wantSenderID:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()

			imp := &Importer{
				db:            db,
				accountID:     1,
				inboxID:       2,
				agentUserType: tc.agentUserType,
				agentUserID:   tc.agentUserID,
			}

			// Idempotency probe must hit messages with (inbox_id, source_id).
			// Returning ErrNoRows forces the writer to attempt the INSERT.
			mock.ExpectBegin()
			mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, conversation_id FROM messages
			WHERE inbox_id = $1 AND source_id = $2
			LIMIT 1`)).
				WithArgs(2, "WAID:"+tc.msg.ID).
				WillReturnError(noRowsError())

			// The INSERT carries 12 args. Pin the WhatsApp timestamp because
			// issue #580 regressed when Chatwoot stamped history rows with
			// import time instead of the original message time. Keep unrelated
			// formatting arguments loose so this test remains focused.
			mock.ExpectQuery(`INSERT INTO messages`).
				WithArgs(
					sqlmock.AnyArg(), // content
					1,                // account_id
					2,                // inbox_id
					convID,           // conversation_id
					sqlmock.AnyArg(), // message_type
					tc.msg.Timestamp, // created_at and updated_at both use this placeholder
					sqlmock.AnyArg(), // status
					"WAID:"+tc.msg.ID,
					0,                              // content_type (text)
					nullableArg(tc.wantSenderType), // sender_type
					nullableArg(tc.wantSenderID),   // sender_id
					sqlmock.AnyArg(),               // additional_attributes
				).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			mock.ExpectCommit()

			tx, err := imp.db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("BeginTx: %v", err)
			}
			_, wrote, err := imp.insertMessage(context.Background(), tx, convID, contactID, tc.msg, false)
			if err != nil {
				t.Fatalf("insertMessage: %v", err)
			}
			if !wrote {
				t.Errorf("insertMessage wrote=false; want true (idempotency probe should miss)")
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unmet sqlmock expectations: %v", err)
			}
		})
	}
}

// nullableArg returns a sqlmock argument matcher for either a concrete
// expected value or a SQL-level NULL, since lib/pq passes sql.Null{String,Int64}
// through as their underlying value or nil. database/sql turns
// sql.NullString{Valid:false} into the empty string and sql.NullInt64{Valid:false}
// into 0 from the driver's perspective; sqlmock unfortunately compares the
// rendered driver value, so a NULL-expecting case has to match nil.
func nullableArg(want any) any {
	if want == nil {
		return nil
	}
	return want
}

// noRowsError returns sql.ErrNoRows. The wrapper exists so call sites read
// as "expect the idempotency probe to miss" rather than referencing the
// generic package-level constant inline.
func noRowsError() error {
	return sql.ErrNoRows
}
