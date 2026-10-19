package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

type store struct {
	db                        *sql.DB
	maxClients                int
	maxRegistrationsPerWindow int
	registrationWindow        time.Duration
	unusedClientTTL           time.Duration
}

const (
	defaultMaxClients                = 1000
	defaultMaxRegistrationsPerWindow = 40
	defaultRegistrationWindow        = time.Hour
	defaultUnusedClientTTL           = 24 * time.Hour
)

const oauthSchema = `
CREATE TABLE IF NOT EXISTS oauth_clients (
    client_id TEXT PRIMARY KEY,
    client_name TEXT NOT NULL,
    redirect_uris_json TEXT NOT NULL,
    application_type TEXT NOT NULL,
    token_endpoint_auth_method TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_oauth_clients_created_at ON oauth_clients(created_at);
CREATE TABLE IF NOT EXISTS oauth_authorization_codes (
    code_hash TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    resource TEXT NOT NULL,
    scope TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    used_at INTEGER,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires_at ON oauth_authorization_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_codes_client_id ON oauth_authorization_codes(client_id);
CREATE TABLE IF NOT EXISTS oauth_tokens (
    token_hash TEXT PRIMARY KEY,
    token_type TEXT NOT NULL CHECK (token_type IN ('access', 'refresh')),
    family_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    resource TEXT NOT NULL,
    scope TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_family ON oauth_tokens(family_id);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_expires_at ON oauth_tokens(expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_client_id ON oauth_tokens(client_id);
`

func openStore(uri string) (*store, error) {
	db, err := sql.Open(sqlite.DriverName, sqlite.FormatChatStorageURI(uri, true, true))
	if err != nil {
		return nil, fmt.Errorf("open oauth storage: %w", err)
	}
	// OAuth writes are low-volume and transaction-sensitive. One connection
	// avoids SQLite write races while still keeping WAL enabled for durability.
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping oauth storage: %w", err)
	}
	if _, err := db.ExecContext(ctx, oauthSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize oauth storage: %w", err)
	}
	return &store{
		db:                        db,
		maxClients:                defaultMaxClients,
		maxRegistrationsPerWindow: defaultMaxRegistrationsPerWindow,
		registrationWindow:        defaultRegistrationWindow,
		unusedClientTTL:           defaultUnusedClientTTL,
	}, nil
}

func (s *store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *store) createClient(ctx context.Context, client Client) error {
	return insertClient(ctx, s.db, client)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertClient(ctx context.Context, ex execer, client Client) error {
	redirects, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return fmt.Errorf("encode oauth redirect URIs: %w", err)
	}
	_, err = ex.ExecContext(ctx, `
INSERT INTO oauth_clients (
    client_id, client_name, redirect_uris_json, application_type,
    token_endpoint_auth_method, created_at
) VALUES (?, ?, ?, ?, ?, ?)`,
		client.ID,
		client.Name,
		string(redirects),
		client.ApplicationType,
		client.TokenEndpointAuthMethod,
		client.CreatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("store oauth client: %w", err)
	}
	return nil
}

// registerClient applies durable global bounds to the unauthenticated DCR
// surface. The database-backed window works across process restarts, and the
// total cap prevents registrations from growing the SQLite file indefinitely.
func (s *store) registerClient(ctx context.Context, client Client) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := cleanupExpiredTx(ctx, tx, client.CreatedAt); err != nil {
		return err
	}
	if err := cleanupUnusedClientsTx(ctx, tx, client.CreatedAt.Add(-s.unusedClientTTL)); err != nil {
		return err
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_clients`).Scan(&total); err != nil {
		return err
	}
	if total >= s.maxClients {
		return ErrRegistrationLimit
	}
	var recent int
	windowStart := client.CreatedAt.Add(-s.registrationWindow).Unix()
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM oauth_clients WHERE created_at > ?`, windowStart).Scan(&recent); err != nil {
		return err
	}
	if recent >= s.maxRegistrationsPerWindow {
		return ErrRegistrationLimit
	}

	if err := insertClient(ctx, tx, client); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *store) getClient(ctx context.Context, clientID string) (Client, error) {
	var (
		client        Client
		redirectsJSON string
		createdAt     int64
	)
	err := s.db.QueryRowContext(ctx, `
SELECT client_id, client_name, redirect_uris_json, application_type,
       token_endpoint_auth_method, created_at
FROM oauth_clients WHERE client_id = ?`, clientID).Scan(
		&client.ID,
		&client.Name,
		&redirectsJSON,
		&client.ApplicationType,
		&client.TokenEndpointAuthMethod,
		&createdAt,
	)
	if err != nil {
		return Client{}, err
	}
	if err := json.Unmarshal([]byte(redirectsJSON), &client.RedirectURIs); err != nil {
		return Client{}, fmt.Errorf("decode oauth redirect URIs: %w", err)
	}
	client.CreatedAt = time.Unix(createdAt, 0).UTC()
	return client, nil
}

func (s *store) issueAuthorizationCode(ctx context.Context, grant AuthorizationGrant, now time.Time, ttl time.Duration) (string, error) {
	code, err := randomSecret("gowa_code_", 32)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if err := cleanupExpiredTx(ctx, tx, now); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO oauth_authorization_codes (
    code_hash, client_id, subject, redirect_uri, code_challenge,
    resource, scope, expires_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		hashSecret(code),
		grant.ClientID,
		grant.Subject,
		grant.RedirectURI,
		grant.CodeChallenge,
		grant.Resource,
		grant.Scope,
		now.Add(ttl).Unix(),
		now.Unix(),
	)
	if err != nil {
		return "", fmt.Errorf("store authorization code: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return code, nil
}

func (s *store) exchangeAuthorizationCode(
	ctx context.Context,
	req CodeExchange,
	now time.Time,
	accessTTL time.Duration,
	refreshTTL time.Duration,
) (TokenPair, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenPair{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := cleanupExpiredTx(ctx, tx, now); err != nil {
		return TokenPair{}, err
	}

	var (
		clientID      string
		subject       string
		redirectURI   string
		codeChallenge string
		resource      string
		scope         string
		expiresAt     int64
		usedAt        sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
SELECT client_id, subject, redirect_uri, code_challenge, resource, scope,
       expires_at, used_at
FROM oauth_authorization_codes
WHERE code_hash = ?`, hashSecret(req.Code)).Scan(
		&clientID,
		&subject,
		&redirectURI,
		&codeChallenge,
		&resource,
		&scope,
		&expiresAt,
		&usedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TokenPair{}, ErrInvalidGrant
	}
	if err != nil {
		return TokenPair{}, err
	}
	if usedAt.Valid || now.Unix() >= expiresAt || clientID != req.ClientID || redirectURI != req.RedirectURI {
		return TokenPair{}, ErrInvalidGrant
	}
	if resource != req.Resource {
		return TokenPair{}, ErrInvalidTarget
	}
	if !verifyPKCES256(req.CodeVerifier, codeChallenge) {
		return TokenPair{}, ErrInvalidGrant
	}

	result, err := tx.ExecContext(ctx, `
UPDATE oauth_authorization_codes
SET used_at = ?
WHERE code_hash = ? AND used_at IS NULL`, now.Unix(), hashSecret(req.Code))
	if err != nil {
		return TokenPair{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return TokenPair{}, ErrInvalidGrant
	}

	familyID, err := randomSecret("gowa_family_", 18)
	if err != nil {
		return TokenPair{}, err
	}
	pair, err := issueTokenPairTx(ctx, tx, familyID, clientID, subject, resource, scope, now, accessTTL, refreshTTL)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(); err != nil {
		return TokenPair{}, err
	}
	return pair, nil
}

func (s *store) rotateRefreshToken(
	ctx context.Context,
	req RefreshExchange,
	now time.Time,
	accessTTL time.Duration,
	refreshTTL time.Duration,
) (TokenPair, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenPair{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := cleanupExpiredTx(ctx, tx, now); err != nil {
		return TokenPair{}, err
	}

	var (
		familyID  string
		clientID  string
		subject   string
		resource  string
		scope     string
		expiresAt int64
		revokedAt sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
SELECT family_id, client_id, subject, resource, scope, expires_at, revoked_at
FROM oauth_tokens
WHERE token_hash = ? AND token_type = 'refresh'`, hashSecret(req.RefreshToken)).Scan(
		&familyID,
		&clientID,
		&subject,
		&resource,
		&scope,
		&expiresAt,
		&revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return TokenPair{}, ErrInvalidGrant
	}
	if err != nil {
		return TokenPair{}, err
	}
	if revokedAt.Valid {
		if _, err := tx.ExecContext(ctx, `
UPDATE oauth_tokens
SET revoked_at = ?
WHERE family_id = ? AND revoked_at IS NULL`, now.Unix(), familyID); err != nil {
			return TokenPair{}, err
		}
		if err := tx.Commit(); err != nil {
			return TokenPair{}, err
		}
		return TokenPair{}, ErrRefreshReuse
	}
	if now.Unix() >= expiresAt || clientID != req.ClientID {
		return TokenPair{}, ErrInvalidGrant
	}
	if resource != req.Resource {
		return TokenPair{}, ErrInvalidTarget
	}

	result, err := tx.ExecContext(ctx, `
UPDATE oauth_tokens
SET revoked_at = ?
WHERE token_hash = ? AND token_type = 'refresh' AND revoked_at IS NULL`,
		now.Unix(), hashSecret(req.RefreshToken))
	if err != nil {
		return TokenPair{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return TokenPair{}, ErrInvalidGrant
	}

	pair, err := issueTokenPairTx(ctx, tx, familyID, clientID, subject, resource, scope, now, accessTTL, refreshTTL)
	if err != nil {
		return TokenPair{}, err
	}
	if err := tx.Commit(); err != nil {
		return TokenPair{}, err
	}
	return pair, nil
}

func cleanupExpiredTx(ctx context.Context, tx *sql.Tx, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_authorization_codes WHERE expires_at <= ?`, now.Unix()); err != nil {
		return fmt.Errorf("delete expired authorization codes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE expires_at <= ?`, now.Unix()); err != nil {
		return fmt.Errorf("delete expired oauth tokens: %w", err)
	}
	return nil
}

func cleanupUnusedClientsTx(ctx context.Context, tx *sql.Tx, cutoff time.Time) error {
	if _, err := tx.ExecContext(ctx, `
DELETE FROM oauth_clients
WHERE created_at <= ?
  AND NOT EXISTS (
      SELECT 1 FROM oauth_authorization_codes
      WHERE oauth_authorization_codes.client_id = oauth_clients.client_id
  )
  AND NOT EXISTS (
      SELECT 1 FROM oauth_tokens
      WHERE oauth_tokens.client_id = oauth_clients.client_id
  )`, cutoff.Unix()); err != nil {
		return fmt.Errorf("delete unused oauth clients: %w", err)
	}
	return nil
}

func (s *store) validateAccessToken(ctx context.Context, rawToken, resource string, now time.Time) (Principal, error) {
	var (
		principal    Principal
		storedTarget string
		expiresAt    int64
		revokedAt    sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
SELECT subject, client_id, scope, resource, expires_at, revoked_at
FROM oauth_tokens
WHERE token_hash = ? AND token_type = 'access'`, hashSecret(rawToken)).Scan(
		&principal.Subject,
		&principal.ClientID,
		&principal.Scope,
		&storedTarget,
		&expiresAt,
		&revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrInvalidToken
	}
	if err != nil {
		return Principal{}, err
	}
	if revokedAt.Valid || now.Unix() >= expiresAt || storedTarget != resource {
		return Principal{}, ErrInvalidToken
	}
	return principal, nil
}

func issueTokenPairTx(
	ctx context.Context,
	tx *sql.Tx,
	familyID,
	clientID,
	subject,
	resource,
	scope string,
	now time.Time,
	accessTTL,
	refreshTTL time.Duration,
) (TokenPair, error) {
	accessToken, err := randomSecret("gowa_at_", 32)
	if err != nil {
		return TokenPair{}, err
	}
	refreshToken, err := randomSecret("gowa_rt_", 32)
	if err != nil {
		return TokenPair{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO oauth_tokens (
    token_hash, token_type, family_id, client_id, subject, resource, scope,
    expires_at, created_at
) VALUES (?, 'access', ?, ?, ?, ?, ?, ?, ?)`,
		hashSecret(accessToken), familyID, clientID, subject, resource, scope,
		now.Add(accessTTL).Unix(), now.Unix()); err != nil {
		return TokenPair{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO oauth_tokens (
    token_hash, token_type, family_id, client_id, subject, resource, scope,
    expires_at, created_at
) VALUES (?, 'refresh', ?, ?, ?, ?, ?, ?, ?)`,
		hashSecret(refreshToken), familyID, clientID, subject, resource, scope,
		now.Add(refreshTTL).Unix(), now.Unix()); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(accessTTL / time.Second),
		Scope:        scope,
	}, nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func verifyPKCES256(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func randomSecret(prefix string, byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate oauth secret: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}
