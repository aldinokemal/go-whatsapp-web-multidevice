package cmd

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// fatalExit stands in for the process exit of logrus.Fatal.
type fatalExit struct{ code int }

// A chat storage migration that fails stops the startup before any WhatsApp
// store or client is set up: their message writes need the current schema.
func TestInitAppStopsWhenTheChatStorageSchemaFails(t *testing.T) {
	dir := t.TempDir()
	chatDB := filepath.Join(dir, "chatstorage.db")
	db, err := sql.Open(sqlite.DriverName, chatDB)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// At version 49, with the column that migration 50 adds already there.
	for _, statement := range []string{
		`CREATE TABLE schema_info (version INTEGER PRIMARY KEY, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO schema_info (version) VALUES (49)`,
		`CREATE TABLE messages (id VARCHAR(255), quoted_media BLOB)`,
	} {
		_, err := db.Exec(statement)
		require.NoError(t, err)
	}

	// initApp creates its folders under the working directory.
	t.Chdir(dir)
	for setting, value := range map[*string]string{
		&config.PathQrCode:     "qrcode",
		&config.PathSendItems:  "senditems",
		&config.PathMedia:      "media",
		&config.PathStorages:   "storages",
		&config.PathUICache:    "ui",
		&config.ChatStorageURI: "file:" + chatDB,
		// InitWaDB panics on this URI: reaching it fails the test.
		&config.DBURI:     "unreachable:whatsapp",
		&config.DBKeysURI: "",
	} {
		previous := *setting
		*setting = value
		t.Cleanup(func() { *setting = previous })
	}
	previousDB, previousRepo := chatStorageDB, chatStorageRepo
	t.Cleanup(func() {
		if chatStorageDB != previousDB {
			_ = chatStorageDB.Close()
		}
		chatStorageDB, chatStorageRepo = previousDB, previousRepo
	})
	logger := logrus.StandardLogger()
	var logs bytes.Buffer
	previousOut, previousExit := logger.Out, logger.ExitFunc
	logger.SetOutput(&logs)
	logger.ExitFunc = func(code int) { panic(fatalExit{code}) }
	t.Cleanup(func() {
		logger.SetOutput(previousOut)
		logger.ExitFunc = previousExit
	})

	stopped := func() (stopped any) {
		defer func() { stopped = recover() }()
		initApp()
		return nil
	}()

	require.Equal(t, fatalExit{1}, stopped)
	require.Contains(t, logs.String(), "failed to initialize chat storage schema: failed to run migration 50")
	var version int
	require.NoError(t, db.QueryRow(`SELECT MAX(version) FROM schema_info`).Scan(&version))
	require.Equal(t, 49, version)
}
