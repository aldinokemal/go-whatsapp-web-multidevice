package pgimport

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestUpsertContact_PreservesExistingIndividualName(t *testing.T) {
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const jid = "6281234567890@s.whatsapp.net"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, jid).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(200))
	mock.ExpectRollback()

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.upsertContact(context.Background(), tx, jid, "Alice WA", false)
	if err != nil {
		t.Fatalf("upsertContact: %v", err)
	}
	if id != 200 {
		t.Fatalf("contact id = %d, want 200", id)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func TestUpsertContact_RefreshesExistingGroupName(t *testing.T) {
	imp, mock, cleanup := newUpsertContactTestImporter(t)
	defer cleanup()

	const groupJID = "120363123456789@g.us"
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(upsertContactByJIDSQL)).
		WithArgs(imp.accountID, groupJID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(300))
	mock.ExpectExec(regexp.QuoteMeta(refreshContactNameSQL)).
		WithArgs("New Group", 300, imp.accountID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	tx, err := imp.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	id, err := imp.upsertContact(context.Background(), tx, groupJID, "New Group", true)
	if err != nil {
		t.Fatalf("upsertContact: %v", err)
	}
	if id != 300 {
		t.Fatalf("contact id = %d, want 300", id)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sqlmock expectations: %v", err)
	}
}

func newUpsertContactTestImporter(t *testing.T) (*Importer, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
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

const upsertContactByJIDSQL = `
		SELECT id
		FROM contacts
		WHERE account_id = $1
		  AND (custom_attributes->>'gowa_whatsapp_jid') = $2
		LIMIT 1
	`

const refreshContactNameSQL = `
				UPDATE contacts
				SET name = $1, updated_at = now()
				WHERE id = $2 AND account_id = $3 AND (name IS DISTINCT FROM $1)
			`
