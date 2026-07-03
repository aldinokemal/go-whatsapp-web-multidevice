// Package pgimport implements the direct-Postgres history importer for
// Chatwoot. Instead of relaying historical WhatsApp messages through
// Chatwoot's public REST API (which stamps server time on every message),
// it INSERTs directly into Chatwoot's Rails schema so that `created_at`,
// sender identity, and group metadata are preserved faithfully.
//
// Only the historical-sync path uses this package. Live message forwarding
// and the Chatwoot → WhatsApp reply path continue to use the REST client
// in the parent package, so Chatwoot's normal event pipeline (assignment
// rules, automations, webhooks, agent UI sockets) keeps firing on inbound
// traffic exactly as before.
//
// The package is opt-in via config.ChatwootImportDBURI. When unset, the
// parent SyncService uses the REST importer and behavior is identical to
// earlier releases. When set, a broken DB connection is treated as a sync
// failure instead of falling back silently.
package pgimport

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"
)

// Importer owns a pooled Postgres connection to the Chatwoot database and
// the account/inbox identifiers that every INSERT is scoped to.
//
// An Importer is safe for concurrent use. The underlying *sql.DB has its
// own connection pool; callers should re-use a single Importer per process.
type Importer struct {
	db        *sql.DB
	accountID int
	inboxID   int

	// schemaVersion is the latest Chatwoot Rails migration version observed
	// on startup. Stored for diagnostics only; writes do not gate on it.
	schemaVersion string

	// agentUserType / agentUserID identify the Chatwoot user that owns the
	// configured API token. Used to stamp outgoing imported messages so they
	// render with the agent's name and avatar in the agent UI instead of as
	// "Unknown sender". Resolved best-effort from access_tokens at startup;
	// when unset (token absent or table unreachable), outgoing messages fall
	// back to NULL sender — the row still imports, just without attribution.
	agentUserType string
	agentUserID   int64

	once   sync.Once
	closed atomic.Bool
}

// Config is the minimal input required to open an Importer.
type Config struct {
	// DSN is a libpq connection string, e.g.
	//   postgresql://postgres:secret@chatwoot-db:5432/chatwoot_production?sslmode=disable
	DSN string

	// AccountID and InboxID must match the Chatwoot account and inbox that
	// the parent REST client (client.go) is configured against. The
	// importer refuses to open if either is zero.
	AccountID int
	InboxID   int

	// APIToken is the Chatwoot REST API token (same one the parent REST
	// client uses). Optional — when present, the importer resolves the
	// owning Chatwoot user from `access_tokens` so outgoing imported
	// messages can be stamped with that agent (sender_type='User',
	// sender_id=<user_id>). Without it, outgoing rows are written with NULL
	// sender, which Chatwoot displays as "Unknown sender".
	APIToken string
}

// New opens a Postgres pool to the Chatwoot database and verifies that the
// tables the importer writes to actually exist. It does *not* check the
// migration version range: the same write pattern has held across
// Chatwoot 2.x → 4.x, and additive schema changes should keep
// working. If the schema has diverged in a way we can't write against, the
// error surfaces on the first INSERT.
func New(ctx context.Context, cfg Config) (*Importer, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("pgimport: empty DSN")
	}
	if cfg.AccountID == 0 || cfg.InboxID == 0 {
		return nil, fmt.Errorf("pgimport: AccountID and InboxID must be non-zero")
	}

	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("pgimport: open: %w", err)
	}

	db.SetMaxOpenConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("pgimport: ping: %w", err)
	}

	imp := &Importer{
		db:        db,
		accountID: cfg.AccountID,
		inboxID:   cfg.InboxID,
	}

	if err := imp.verifyTables(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	imp.schemaVersion = imp.readSchemaVersion(ctx)
	if err := imp.verifyAccountInbox(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	imp.resolveAgent(ctx, cfg.APIToken)

	logrus.Infof("Chatwoot pgimport: connected (account=%d inbox=%d schema_version=%s agent=%s/%d)",
		imp.accountID, imp.inboxID, imp.schemaVersion, imp.agentUserType, imp.agentUserID)
	return imp, nil
}

// Close releases the underlying pool. Safe to call more than once.
func (i *Importer) Close() error {
	var err error
	i.once.Do(func() {
		i.closed.Store(true)
		err = i.db.Close()
	})
	return err
}

// verifyTables confirms that every table the writer touches exists and is
// reachable. We fail fast here rather than halfway through an import.
func (i *Importer) verifyTables(ctx context.Context) error {
	required := []string{
		"accounts",
		"inboxes",
		"contacts",
		"contact_inboxes",
		"conversations",
		"messages",
	}
	for _, name := range required {
		var exists bool
		err := i.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = current_schema()
				  AND table_name = $1
			)`, name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("pgimport: verify table %q: %w", name, err)
		}
		if !exists {
			return fmt.Errorf("pgimport: required Chatwoot table %q not found; is CHATWOOT_IMPORT_DB_URI pointing at the right database?", name)
		}
	}
	return nil
}

// verifyAccountInbox confirms the configured (account, inbox) pair exists
// before any import writes start. The direct importer writes every row with
// these IDs, so a mismatch is configuration failure and should not degrade
// into later per-row insert errors.
func (i *Importer) verifyAccountInbox(ctx context.Context) error {
	var ok bool
	err := i.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM inboxes
			WHERE id = $1 AND account_id = $2
		)`, i.inboxID, i.accountID).Scan(&ok)
	if err != nil {
		return fmt.Errorf("pgimport: verify inbox=%d account=%d: %w", i.inboxID, i.accountID, err)
	}
	if !ok {
		return fmt.Errorf("pgimport: inbox=%d not found under account=%d", i.inboxID, i.accountID)
	}
	return nil
}

// resolveAgent looks up the Chatwoot user that owns the given API token in
// the access_tokens table. The result is cached on the Importer and used
// to stamp outgoing imported messages (sender_type='User', sender_id=<id>)
// so they render with the agent's name and avatar in Chatwoot's UI.
//
// Best-effort: if the token is empty, the row is missing, or the query
// fails, the importer leaves agentUserType/agentUserID zero-valued and
// outgoing messages fall back to NULL sender.
func (i *Importer) resolveAgent(ctx context.Context, apiToken string) {
	if apiToken == "" {
		logrus.Warnf("Chatwoot pgimport: no API token configured; outgoing imported messages will have NULL sender")
		return
	}
	var ownerType sql.NullString
	var ownerID sql.NullInt64
	err := i.db.QueryRowContext(ctx, `
		SELECT owner_type, owner_id
		FROM access_tokens
		WHERE token = $1
		LIMIT 1
	`, apiToken).Scan(&ownerType, &ownerID)
	if err != nil {
		// sql.ErrNoRows or any other error — non-fatal, just degrade attribution.
		logrus.Warnf("Chatwoot pgimport: agent lookup from access_tokens failed (outgoing messages will have NULL sender): %v", err)
		return
	}
	if !ownerType.Valid || !ownerID.Valid || ownerID.Int64 == 0 {
		logrus.Warnf("Chatwoot pgimport: access_tokens row has empty owner; outgoing messages will have NULL sender")
		return
	}
	i.agentUserType = ownerType.String
	i.agentUserID = ownerID.Int64
}

// readSchemaVersion returns the latest row from schema_migrations. It is
// purely informational — if the table is unreachable we return an empty
// string and continue.
func (i *Importer) readSchemaVersion(ctx context.Context) string {
	var v sql.NullString
	err := i.db.QueryRowContext(ctx, `
		SELECT version
		FROM schema_migrations
		ORDER BY version DESC
		LIMIT 1
	`).Scan(&v)
	if err != nil {
		return ""
	}
	return v.String
}
