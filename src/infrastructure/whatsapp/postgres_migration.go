package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// MigratedSuffix is appended to the legacy SQLite session file once its sessions live
// in Postgres, so a later boot never imports them again (a device removed in Postgres
// must not come back from an old file).
const MigratedSuffix = ".migrated-to-postgres"

// MigrateSQLiteSessionsToPostgres copies the whatsmeow session store from a legacy
// SQLite file into Postgres, once, so switching --db-uri to Postgres keeps every paired
// device logged in instead of asking each phone to scan the QR code again.
//
// It runs only when sqlitePath exists and the Postgres store has no device yet. The
// SQLite store is first upgraded to the running whatsmeow schema, then every
// whatsmeow_* table is copied column by column inside one Postgres transaction —
// either all sessions arrive or none do. On success the SQLite file is renamed with
// MigratedSuffix. pg must already hold the upgraded schema (call after InitWaDB).
func MigrateSQLiteSessionsToPostgres(ctx context.Context, sqlitePath string, pg *sql.DB, dbLog waLog.Logger) (int, error) {
	if _, err := os.Stat(sqlitePath); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var existing int
	if err := pg.QueryRowContext(ctx, `SELECT COUNT(*) FROM whatsmeow_device`).Scan(&existing); err != nil {
		return 0, fmt.Errorf("count postgres devices: %w", err)
	}
	if existing > 0 {
		logrus.Warnf("[DB_MIGRATION] postgres already holds %d session(s); legacy %s left untouched", existing, sqlitePath)
		return 0, nil
	}

	uri := sqlite.FormatChatStorageURI("file:"+sqlitePath, true, true)
	legacy, err := sqlstore.New(ctx, sqlite.DriverName, uri, dbLog)
	if err != nil {
		return 0, fmt.Errorf("upgrade legacy sqlite store: %w", err)
	}
	_ = legacy.Close()

	src, err := sql.Open(sqlite.DriverName, uri)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	tables, err := legacyTables(ctx, src)
	if err != nil {
		return 0, err
	}

	tx, err := pg.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	devices := 0
	for _, table := range tables {
		n, err := copyTable(ctx, src, tx, table)
		if err != nil {
			return 0, fmt.Errorf("copy %s: %w", table, err)
		}
		if table == "whatsmeow_device" {
			devices = n
		}
		logrus.Infof("[DB_MIGRATION] %s: %d row(s)", table, n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	_ = src.Close()

	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Rename(sqlitePath+suffix, sqlitePath+suffix+MigratedSuffix); err != nil && !os.IsNotExist(err) {
			return devices, fmt.Errorf("sessions copied, but renaming %s failed: %w", sqlitePath+suffix, err)
		}
	}
	logrus.Infof("[DB_MIGRATION] %d device session(s) moved from %s to postgres", devices, sqlitePath)
	return devices, nil
}

// legacyTables lists the whatsmeow tables of the SQLite store, devices first: every
// other table references whatsmeow_device.
func legacyTables(ctx context.Context, src *sql.DB) ([]string, error) {
	rows, err := src.QueryContext(ctx, `SELECT name FROM sqlite_master
		WHERE type = 'table' AND name LIKE 'whatsmeow_%' AND name <> 'whatsmeow_version' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := []string{"whatsmeow_device"}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if name != "whatsmeow_device" {
			tables = append(tables, name)
		}
	}
	return tables, rows.Err()
}

func copyTable(ctx context.Context, src *sql.DB, dst *sql.Tx, table string) (int, error) {
	pgTypes := map[string]string{}
	typeRows, err := dst.QueryContext(ctx, `SELECT column_name, data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return 0, err
	}
	for typeRows.Next() {
		var name, kind string
		if err := typeRows.Scan(&name, &kind); err != nil {
			typeRows.Close()
			return 0, err
		}
		pgTypes[name] = kind
	}
	typeRows.Close()
	if len(pgTypes) == 0 {
		return 0, fmt.Errorf("table missing in postgres")
	}

	rows, err := src.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM %q`, table))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	quoted := make([]string, len(columns))
	params := make([]string, len(columns))
	for i, column := range columns {
		if _, ok := pgTypes[column]; !ok {
			return 0, fmt.Errorf("column %s missing in postgres", column)
		}
		quoted[i] = fmt.Sprintf("%q", column)
		params[i] = fmt.Sprintf("$%d", i+1)
	}
	insert := fmt.Sprintf(`INSERT INTO %q (%s) VALUES (%s) ON CONFLICT DO NOTHING`,
		table, strings.Join(quoted, ", "), strings.Join(params, ", "))

	copied := 0
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return copied, err
		}
		for i, column := range columns {
			values[i] = convertForPostgres(values[i], pgTypes[column])
		}
		if _, err := dst.ExecContext(ctx, insert, values...); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, rows.Err()
}

// convertForPostgres bridges SQLite's loose typing: booleans are stored as integers,
// and a TEXT/BLOB value may come back as either string or []byte.
func convertForPostgres(value any, pgType string) any {
	switch pgType {
	case "boolean":
		switch v := value.(type) {
		case int64:
			return v != 0
		case string:
			return v == "1" || strings.EqualFold(v, "true")
		}
	case "bytea":
		if v, ok := value.(string); ok {
			return []byte(v)
		}
	case "text", "character varying":
		if v, ok := value.([]byte); ok {
			return string(v)
		}
	}
	return value
}
