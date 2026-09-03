package chatwoot

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot/pgimport"

	_ "github.com/lib/pq"
)

// These tests run syncChatPG against a real Chatwoot schema. The sqlmock suite
// in pgimport already pins the shape of every statement; what it cannot show is
// how the writer behaves against Chatwoot's actual constraints, defaults and
// unique indexes -- most of all the (inbox_id, source_id) index that
// upsertContactInbox's ON CONFLICT clause depends on.
//
// Skipped unless CHATWOOT_TEST_DB_URI points at a database prepared with
// Chatwoot's own migrations and seeded with the account and inbox below.
// `go test ./...` stays green without it.
//
// To prepare one (pgvector is required: Chatwoot's schema declares the
// "vector" extension, so the stock postgres image cannot load it):
//
//	docker network create cw-test
//	docker run -d --name cw-test-pg --network cw-test -p 127.0.0.1:55440:5432 \
//	  -e POSTGRES_USER=chatwoot -e POSTGRES_PASSWORD=chatwoot_test \
//	  -e POSTGRES_DB=chatwoot_test pgvector/pgvector:pg17
//	docker run -d --name cw-test-redis --network cw-test redis:7-alpine
//	docker run --rm --network cw-test -e RAILS_ENV=production \
//	  -e POSTGRES_HOST=cw-test-pg -e POSTGRES_USERNAME=chatwoot \
//	  -e POSTGRES_PASSWORD=chatwoot_test -e POSTGRES_DATABASE=chatwoot_test \
//	  -e REDIS_URL=redis://cw-test-redis:6379 -e SECRET_KEY_BASE=$(openssl rand -hex 32) \
//	  chatwoot/chatwoot:latest bundle exec rails db:chatwoot_prepare
//
// Then seed the fixture the tests expect (account 1, inbox 2 on an API
// channel), and run:
//
//	psql "$CHATWOOT_TEST_DB_URI" <<'SQL'
//	INSERT INTO accounts (id, name, created_at, updated_at)
//	  VALUES (1, 'gowa test', now(), now());
//	INSERT INTO channel_api (id, account_id, identifier, created_at, updated_at)
//	  VALUES (1, 1, 'gowa-test-channel', now(), now());
//	INSERT INTO inboxes (id, account_id, channel_id, channel_type, name, created_at, updated_at)
//	  VALUES (2, 1, 1, 'Channel::Api', 'gowa test inbox', now(), now());
//	SQL
//
//	CHATWOOT_TEST_DB_URI="postgresql://chatwoot:chatwoot_test@127.0.0.1:55440/chatwoot_test?sslmode=disable" \
//	  go test -run SyncChatPGIntegration ./infrastructure/chatwoot/ -v
const (
	pgIntegrationEnv       = "CHATWOOT_TEST_DB_URI"
	pgIntegrationAccountID = 1
	pgIntegrationInboxID   = 2
)

// pgIntegrationContext bounds everything a test does against Postgres. Without
// a deadline an unreachable or wedged server would leave the run blocked until
// the whole `go test` binary times out, reporting nothing useful about which
// statement stalled.
func pgIntegrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// pgIntegrationDB opens the database named by CHATWOOT_TEST_DB_URI, skipping the
// test when it is unset.
func pgIntegrationDB(t *testing.T, ctx context.Context) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv(pgIntegrationEnv)
	if dsn == "" {
		t.Skipf("%s not set; skipping the Chatwoot Postgres integration test", pgIntegrationEnv)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", pgIntegrationEnv, err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		t.Fatalf("ping %s: %v", pgIntegrationEnv, err)
	}
	return db, dsn
}

// truncateChatwootData clears the rows an import writes while leaving the
// account, inbox and agent fixture in place, so each test starts from a known
// empty inbox without re-running the migrations.
func truncateChatwootData(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `TRUNCATE messages, conversations, contact_inboxes, contacts RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// newPGIntegrationImporter opens the direct-Postgres importer the same way the
// REST service does at startup.
func newPGIntegrationImporter(t *testing.T, ctx context.Context, dsn string) *pgimport.Importer {
	t.Helper()
	imp, err := pgimport.New(ctx, pgimport.Config{
		DSN:       dsn,
		AccountID: pgIntegrationAccountID,
		InboxID:   pgIntegrationInboxID,
	})
	if err != nil {
		t.Fatalf("pgimport.New: %v", err)
	}
	t.Cleanup(func() { _ = imp.Close() })
	return imp
}

// pgIntegrationService wires a SyncService whose REST calls all fail, so any
// REST traffic a test observes is traffic the production code chose to make.
func pgIntegrationService(t *testing.T, repo *chatwootSyncChatRepo) (*SyncService, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	return NewSyncService(&Client{
		BaseURL:    server.URL,
		APIToken:   "gowa-test-token",
		AccountID:  pgIntegrationAccountID,
		InboxID:    pgIntegrationInboxID,
		HTTPClient: server.Client(),
	}, repo), &requests
}

// countConversationsAndMessages reports what actually landed in Chatwoot's own
// tables, which is the only way to tell an idempotent skip from a silent write.
func countConversationsAndMessages(t *testing.T, ctx context.Context, db *sql.DB) (convCount, msgCount int) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM conversations`).Scan(&convCount); err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM messages`).Scan(&msgCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	return convCount, msgCount
}

// A first import must land the contact, the conversation and the message in
// Chatwoot's own tables, and a second identical run must add nothing. This is
// the baseline the reopen tests below build on.
func TestSyncChatPGIntegration_ImportIsIdempotent(t *testing.T) {
	ctx := pgIntegrationContext(t)
	db, dsn := pgIntegrationDB(t, ctx)
	truncateChatwootData(t, ctx, db)

	msg := chatwootSyncChatMessage("wa-pg-first")
	repo := newChatwootSyncChatRepo(msg)
	svc, _ := pgIntegrationService(t, repo)
	importer := newPGIntegrationImporter(t, ctx, dsn)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	progress := NewSyncProgress(msg.DeviceID)
	if err := svc.syncChatPG(ctx, importer, msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), progress); err != nil {
		t.Fatalf("first syncChatPG: %v", err)
	}
	convs, msgs := countConversationsAndMessages(t, ctx, db)
	if convs != 1 || msgs != 1 {
		t.Fatalf("after first import: conversations=%d messages=%d, want 1 and 1", convs, msgs)
	}

	progress = NewSyncProgress(msg.DeviceID)
	if err := svc.syncChatPG(ctx, importer, msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), progress); err != nil {
		t.Fatalf("second syncChatPG: %v", err)
	}
	convs, msgs = countConversationsAndMessages(t, ctx, db)
	if convs != 1 || msgs != 1 {
		t.Fatalf("after second import: conversations=%d messages=%d, want the row counts unchanged", convs, msgs)
	}
}

// The defect this branch fixes, on the Postgres path. Auto-sync runs on every
// connect and re-imports the whole time window, so a chat whose messages are
// all in Chatwoot already must leave a resolved conversation resolved. An agent
// who closes a thread should not find it reopened after the next restart.
func TestSyncChatPGIntegration_ResolvedConversationStaysResolved(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	ctx := pgIntegrationContext(t)
	db, dsn := pgIntegrationDB(t, ctx)
	truncateChatwootData(t, ctx, db)

	msg := chatwootSyncChatMessage("wa-pg-resolved")
	repo := newChatwootSyncChatRepo(msg)
	svc, _ := pgIntegrationService(t, repo)
	importer := newPGIntegrationImporter(t, ctx, dsn)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	if err := svc.syncChatPG(ctx, importer, msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err != nil {
		t.Fatalf("seed syncChatPG: %v", err)
	}

	// The agent resolves the thread. Chatwoot's `status` enum: 0=open,
	// 1=resolved, 2=pending, 3=snoozed.
	if _, err := db.ExecContext(ctx, `UPDATE conversations SET status = 1`); err != nil {
		t.Fatalf("resolve conversation: %v", err)
	}

	// The next auto-sync sees the same window and the same message.
	if err := svc.syncChatPG(ctx, importer, msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err != nil {
		t.Fatalf("re-sync syncChatPG: %v", err)
	}

	var status int
	if err := db.QueryRowContext(ctx, `SELECT status FROM conversations`).Scan(&status); err != nil {
		t.Fatalf("read conversation status: %v", err)
	}
	if status != 1 {
		t.Fatalf("conversation status = %d, want 1 (resolved); the re-import reopened a thread with nothing new to add", status)
	}
}

// A genuinely new message must still reach Chatwoot, and reopening the thread
// for it is the configured, wanted behaviour. This is the guard against
// "fixing" the reopen by never importing again.
func TestSyncChatPGIntegration_NewMessageStillImportsAndReopens(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	ctx := pgIntegrationContext(t)
	db, dsn := pgIntegrationDB(t, ctx)
	truncateChatwootData(t, ctx, db)

	first := chatwootSyncChatMessage("wa-pg-old")
	repo := newChatwootSyncChatRepo(first)
	svc, _ := pgIntegrationService(t, repo)
	importer := newPGIntegrationImporter(t, ctx, dsn)
	chat := &domainChatStorage.Chat{JID: first.ChatJID, Name: "Contact"}

	if err := svc.syncChatPG(ctx, importer, first.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(first.DeviceID)); err != nil {
		t.Fatalf("seed syncChatPG: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE conversations SET status = 1`); err != nil {
		t.Fatalf("resolve conversation: %v", err)
	}

	// A message arrives that Chatwoot has never seen.
	second := chatwootSyncChatMessage("wa-pg-new")
	second.Timestamp = first.Timestamp.Add(time.Hour)
	repo.messages = []*domainChatStorage.Message{first, second}

	if err := svc.syncChatPG(ctx, importer, second.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(second.DeviceID)); err != nil {
		t.Fatalf("re-sync syncChatPG: %v", err)
	}

	_, msgs := countConversationsAndMessages(t, ctx, db)
	if msgs != 2 {
		t.Fatalf("messages = %d, want 2; the new message must still be imported", msgs)
	}
	var status int
	if err := db.QueryRowContext(ctx, `SELECT status FROM conversations`).Scan(&status); err != nil {
		t.Fatalf("read conversation status: %v", err)
	}
	if status == 1 {
		t.Fatal("conversation is still resolved; a genuinely new message must reopen it when ChatwootReopenConversation is set")
	}
}

// The durability gap: the Chatwoot transaction commits before the local
// message links are persisted, so a crash or chatstorage failure in between
// leaves the message in Chatwoot with no local link. The prefilter then reports
// the row as pending on the next sync and ImportChat runs. Its idempotency
// probe skips the row -- but reopening used to happen before that probe, so
// the thread came back with nothing added. The reopen now waits for an actual
// write, and the skipped row's link repairs the local table on the way.
func TestSyncChatPGIntegration_OrphanedChatwootMessageDoesNotReopen(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	ctx := pgIntegrationContext(t)
	db, dsn := pgIntegrationDB(t, ctx)
	truncateChatwootData(t, ctx, db)

	msg := chatwootSyncChatMessage("wa-pg-orphan")
	repo := newChatwootSyncChatRepo(msg)
	svc, _ := pgIntegrationService(t, repo)
	importer := newPGIntegrationImporter(t, ctx, dsn)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	// First sync lands the message in Chatwoot and the link locally.
	if err := svc.syncChatPG(ctx, importer, msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err != nil {
		t.Fatalf("seed syncChatPG: %v", err)
	}
	if link, _ := repo.GetChatwootMessageLinkByWhatsAppID(msg.DeviceID, msg.ID); link == nil {
		t.Fatal("seed: expected the local link to be stored after the first import")
	}

	// Simulate the crash window: Chatwoot kept the row, the local link is gone.
	delete(repo.links, chatwootSyncLinkKey(msg.DeviceID, msg.ID))

	// The agent resolves the thread (1 = resolved).
	if _, err := db.ExecContext(ctx, `UPDATE conversations SET status = 1`); err != nil {
		t.Fatalf("resolve conversation: %v", err)
	}

	// Next auto-sync: same window, same message, no local link to filter it.
	if err := svc.syncChatPG(ctx, importer, msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err != nil {
		t.Fatalf("re-sync syncChatPG: %v", err)
	}

	var status int
	if err := db.QueryRowContext(ctx, `SELECT status FROM conversations`).Scan(&status); err != nil {
		t.Fatalf("read conversation status: %v", err)
	}
	if status != 1 {
		t.Fatalf("conversation status = %d, want 1 (resolved); the importer only skipped the existing source_id yet reopened the thread", status)
	}
	_, msgs := countConversationsAndMessages(t, ctx, db)
	if msgs != 1 {
		t.Fatalf("messages = %d, want 1; the orphaned row must be deduped, not duplicated", msgs)
	}
	// The skip still returned a link, so the local table is repaired.
	if link, _ := repo.GetChatwootMessageLinkByWhatsAppID(msg.DeviceID, msg.ID); link == nil {
		t.Fatal("expected the re-sync to repair the missing local link from the skipped row")
	}
}
