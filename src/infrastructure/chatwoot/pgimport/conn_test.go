package pgimport

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// newTestImporter wires a freshly-mocked *sql.DB into a bare Importer so the
// caller can drive both arrange/assert sides of a sqlmock conversation
// against unit-level methods (resolveAgent, insertMessage, ...) without
// going through the New() startup checks.
func newTestImporter(t *testing.T) (*Importer, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	imp := &Importer{
		db:        db,
		accountID: 1,
		inboxID:   2,
	}
	return imp, mock, func() { _ = db.Close() }
}

// resolveAgentSQL is the single source of truth for what we expect
// resolveAgent to send to Postgres. Keeping it separate from the test bodies
// makes it obvious if an unrelated edit perturbs the query — the tests will
// fail on QueryMatcherEqual rather than silently exercising the wrong SQL.
const resolveAgentSQL = `
		SELECT owner_type, owner_id
		FROM access_tokens
		WHERE token = $1
		LIMIT 1
	`

const verifyAccountInboxSQL = `
		SELECT EXISTS (
			SELECT 1 FROM inboxes
			WHERE id = $1 AND account_id = $2
		)`

func TestVerifyAccountInbox_Found(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(true)
	mock.ExpectQuery(verifyAccountInboxSQL).WithArgs(imp.inboxID, imp.accountID).WillReturnRows(rows)

	if err := imp.verifyAccountInbox(context.Background()); err != nil {
		t.Fatalf("verifyAccountInbox: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestVerifyAccountInbox_MissingFails(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"exists"}).AddRow(false)
	mock.ExpectQuery(verifyAccountInboxSQL).WithArgs(imp.inboxID, imp.accountID).WillReturnRows(rows)

	err := imp.verifyAccountInbox(context.Background())
	if err == nil {
		t.Fatal("verifyAccountInbox error = nil, want missing inbox error")
	}
	if got, want := err.Error(), "pgimport: inbox=2 not found under account=1"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestVerifyAccountInbox_QueryErrorFails(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	queryErr := errors.New("permission denied")
	mock.ExpectQuery(verifyAccountInboxSQL).WithArgs(imp.inboxID, imp.accountID).WillReturnError(queryErr)

	err := imp.verifyAccountInbox(context.Background())
	if !errors.Is(err, queryErr) {
		t.Fatalf("verifyAccountInbox error = %v, want wrapping permission denied", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestResolveAgent_FoundUserSetsFields(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"owner_type", "owner_id"}).AddRow("User", int64(42))
	mock.ExpectQuery(resolveAgentSQL).WithArgs("token-123").WillReturnRows(rows)

	imp.resolveAgent(context.Background(), "token-123")

	if got := imp.agentUserType; got != "User" {
		t.Errorf("agentUserType = %q, want %q", got, "User")
	}
	if got := imp.agentUserID; got != 42 {
		t.Errorf("agentUserID = %d, want %d", got, 42)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestResolveAgent_FoundAgentBotSetsFields(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"owner_type", "owner_id"}).AddRow("AgentBot", int64(7))
	mock.ExpectQuery(resolveAgentSQL).WithArgs("bot-token").WillReturnRows(rows)

	imp.resolveAgent(context.Background(), "bot-token")

	if got := imp.agentUserType; got != "AgentBot" {
		t.Errorf("agentUserType = %q, want AgentBot", got)
	}
	if got := imp.agentUserID; got != 7 {
		t.Errorf("agentUserID = %d, want 7", got)
	}
}

func TestResolveAgent_EmptyTokenSkipsQuery(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	// No expectations registered: any DB query would fail the test, proving
	// resolveAgent returns early and never reaches the connection.
	imp.resolveAgent(context.Background(), "")

	if imp.agentUserType != "" || imp.agentUserID != 0 {
		t.Errorf("expected agent fields zero, got type=%q id=%d", imp.agentUserType, imp.agentUserID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet sqlmock expectations: %v", err)
	}
}

func TestResolveAgent_NotFoundLeavesFieldsZero(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	mock.ExpectQuery(resolveAgentSQL).WithArgs("missing").WillReturnError(sql.ErrNoRows)

	imp.resolveAgent(context.Background(), "missing")

	if imp.agentUserType != "" || imp.agentUserID != 0 {
		t.Errorf("expected zero fields on missing agent, got type=%q id=%d", imp.agentUserType, imp.agentUserID)
	}
}

func TestResolveAgent_QueryErrorLeavesFieldsZero(t *testing.T) {
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	mock.ExpectQuery(resolveAgentSQL).WithArgs("any").WillReturnError(errors.New("boom"))

	imp.resolveAgent(context.Background(), "any")

	if imp.agentUserType != "" || imp.agentUserID != 0 {
		t.Errorf("expected zero fields on query error, got type=%q id=%d", imp.agentUserType, imp.agentUserID)
	}
}

func TestResolveAgent_NullOwnerLeavesFieldsZero(t *testing.T) {
	// Defensive against a malformed access_tokens row: NULL owner_type or
	// owner_id must NOT silently pass through as agentUserType="" and
	// agentUserID=0 while passing the !ownerID.Valid check, otherwise we'd
	// stamp NULL on outgoing messages anyway and waste a DB round-trip.
	imp, mock, cleanup := newTestImporter(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"owner_type", "owner_id"}).AddRow(nil, nil)
	mock.ExpectQuery(resolveAgentSQL).WithArgs("nullrow").WillReturnRows(rows)

	imp.resolveAgent(context.Background(), "nullrow")

	if imp.agentUserType != "" || imp.agentUserID != 0 {
		t.Errorf("expected zero fields on NULL owner row, got type=%q id=%d", imp.agentUserType, imp.agentUserID)
	}
}
