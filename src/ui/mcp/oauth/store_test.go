package oauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorePersistsOnlyHashesForCodesAndTokens(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC)
	client := Client{
		ID:                      "client-1",
		Name:                    "Claude",
		RedirectURIs:            []string{testRedirect},
		ApplicationType:         "web",
		TokenEndpointAuthMethod: "none",
		CreatedAt:               now,
	}
	require.NoError(t, store.createClient(ctx, client))

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, err := store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID:      client.ID,
		Subject:       "user",
		RedirectURI:   testRedirect,
		CodeChallenge: challenge,
		Resource:      testResource,
		Scope:         "mcp",
	}, now, 5*time.Minute)
	require.NoError(t, err)

	var storedCodeHash string
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT code_hash FROM oauth_authorization_codes`).Scan(&storedCodeHash))
	assert.Equal(t, hashSecret(code), storedCodeHash)
	assert.NotEqual(t, code, storedCodeHash)

	pair, err := store.exchangeAuthorizationCode(ctx, CodeExchange{
		ClientID:     client.ID,
		Code:         code,
		RedirectURI:  testRedirect,
		CodeVerifier: verifier,
		Resource:     testResource,
	}, now.Add(time.Minute), time.Hour, 30*24*time.Hour)
	require.NoError(t, err)

	rows, err := store.db.QueryContext(ctx, `SELECT token_hash FROM oauth_tokens ORDER BY token_type`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	var hashes []string
	for rows.Next() {
		var hash string
		require.NoError(t, rows.Scan(&hash))
		hashes = append(hashes, hash)
	}
	require.NoError(t, rows.Err())
	require.Len(t, hashes, 2)
	assert.Contains(t, hashes, hashSecret(pair.AccessToken))
	assert.Contains(t, hashes, hashSecret(pair.RefreshToken))
	assert.NotContains(t, hashes, pair.AccessToken)
	assert.NotContains(t, hashes, pair.RefreshToken)
}

func TestStoreAccessTokenIsBoundToResource(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Now().UTC()
	client := Client{ID: "client-1", Name: "Claude", RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: now}
	require.NoError(t, store.createClient(ctx, client))

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	code, err := store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID: client.ID, Subject: "user", RedirectURI: testRedirect,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), Resource: testResource, Scope: "mcp",
	}, now, time.Minute)
	require.NoError(t, err)
	pair, err := store.exchangeAuthorizationCode(ctx, CodeExchange{
		ClientID: client.ID, Code: code, RedirectURI: testRedirect, CodeVerifier: verifier, Resource: testResource,
	}, now, time.Hour, 30*24*time.Hour)
	require.NoError(t, err)

	principal, err := store.validateAccessToken(ctx, pair.AccessToken, testResource, now.Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, "user", principal.Subject)

	_, err = store.validateAccessToken(ctx, pair.AccessToken, "https://gowa.example.com/other", now.Add(time.Minute))
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestStorePrunesExpiredCodesAndTokensBeforeWrites(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)
	client := Client{ID: "client-1", Name: "Claude", RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: now.Add(-time.Hour)}
	require.NoError(t, store.createClient(ctx, client))
	_, err = store.db.ExecContext(ctx, `
INSERT INTO oauth_authorization_codes (
    code_hash, client_id, subject, redirect_uri, code_challenge,
    resource, scope, expires_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"expired-code", client.ID, "user", testRedirect, "challenge", testResource, "mcp", now.Add(-time.Minute).Unix(), now.Add(-time.Hour).Unix())
	require.NoError(t, err)
	for _, tokenType := range []string{"access", "refresh"} {
		_, err = store.db.ExecContext(ctx, `
INSERT INTO oauth_tokens (
    token_hash, token_type, family_id, client_id, subject, resource, scope,
    expires_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			"expired-"+tokenType, tokenType, "family-1", client.ID, "user", testResource, "mcp", now.Add(-time.Minute).Unix(), now.Add(-time.Hour).Unix())
		require.NoError(t, err)
	}

	_, err = store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID: client.ID, Subject: "user", RedirectURI: testRedirect,
		CodeChallenge: "challenge", Resource: testResource, Scope: "mcp",
	}, now, time.Minute)
	require.NoError(t, err)

	var codes, tokens int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_authorization_codes`).Scan(&codes))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_tokens`).Scan(&tokens))
	assert.Equal(t, 1, codes, "the new code remains after expired codes are removed")
	assert.Zero(t, tokens)
}

func TestStoreRegistrationPrunesStaleUnusedClients(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	store.maxClients = 1
	store.maxRegistrationsPerWindow = 10
	store.unusedClientTTL = time.Hour
	stale := Client{ID: "stale", Name: "Stale", RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: now.Add(-2 * time.Hour)}
	require.NoError(t, store.createClient(ctx, stale))

	fresh := Client{ID: "fresh", Name: "Fresh", RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: now}
	require.NoError(t, store.registerClient(ctx, fresh))

	_, err = store.getClient(ctx, stale.ID)
	assert.ErrorIs(t, err, sql.ErrNoRows)
	stored, err := store.getClient(ctx, fresh.ID)
	require.NoError(t, err)
	assert.Equal(t, fresh.ID, stored.ID)
}

func TestStoreRegistrationRateLimitUsesDurableWindow(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 8, 21, 11, 0, 0, 0, time.UTC)
	store.maxClients = 10
	store.maxRegistrationsPerWindow = 1
	store.registrationWindow = time.Hour
	store.unusedClientTTL = 24 * time.Hour
	client := func(id string, createdAt time.Time) Client {
		return Client{ID: id, Name: id, RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: createdAt}
	}

	require.NoError(t, store.registerClient(ctx, client("first", now)))
	assert.ErrorIs(t, store.registerClient(ctx, client("blocked", now.Add(time.Minute))), ErrRegistrationLimit)
	require.NoError(t, store.registerClient(ctx, client("later", now.Add(time.Hour+time.Second))))

	var clients int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_clients`).Scan(&clients))
	assert.Equal(t, 2, clients)
}

func TestReplayOfExpiredConsumedRefreshRevokesDescendants(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	client := Client{ID: "client-1", Name: "Claude", RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: now}
	require.NoError(t, store.createClient(ctx, client))

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	code, err := store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID: client.ID, Subject: "user", RedirectURI: testRedirect,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		Resource:      testResource, Scope: "mcp",
	}, now, 5*time.Minute)
	require.NoError(t, err)
	ancestor, err := store.exchangeAuthorizationCode(ctx, CodeExchange{
		ClientID: client.ID, Code: code, RedirectURI: testRedirect,
		CodeVerifier: verifier, Resource: testResource,
	}, now, time.Hour, 30*24*time.Hour)
	require.NoError(t, err)

	replacement, err := store.rotateRefreshToken(ctx, RefreshExchange{
		ClientID: client.ID, RefreshToken: ancestor.RefreshToken, Resource: testResource,
	}, now.Add(time.Minute), time.Hour, 30*24*time.Hour)
	require.NoError(t, err)
	child, err := store.rotateRefreshToken(ctx, RefreshExchange{
		ClientID: client.ID, RefreshToken: replacement.RefreshToken, Resource: testResource,
	}, now.Add(2*time.Minute), time.Hour, 30*24*time.Hour)
	require.NoError(t, err)
	require.NotEqual(t, ancestor.RefreshToken, replacement.RefreshToken)
	require.NotEqual(t, replacement.RefreshToken, child.RefreshToken)

	replayAt := now.Add(3 * time.Minute)
	_, err = store.db.ExecContext(ctx, `
UPDATE oauth_tokens SET expires_at = ? WHERE token_hash = ?`,
		replayAt.Add(-time.Minute).Unix(), hashSecret(ancestor.RefreshToken))
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `
INSERT INTO oauth_tokens (
    token_hash, token_type, family_id, client_id, subject, resource, scope,
    expires_at, created_at
) VALUES (?, 'refresh', 'other-family', ?, 'user', ?, 'mcp', ?, ?)`,
		"expired-unused", client.ID, testResource, replayAt.Add(-time.Minute).Unix(), now.Unix())
	require.NoError(t, err)

	_, err = store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID: client.ID, Subject: "user", RedirectURI: testRedirect,
		CodeChallenge: "challenge", Resource: testResource, Scope: "mcp",
	}, replayAt, time.Minute)
	require.NoError(t, err)

	var ancestorLeft, unusedLeft int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_tokens WHERE token_hash = ?`, hashSecret(ancestor.RefreshToken)).Scan(&ancestorLeft))
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_tokens WHERE token_hash = ?`, "expired-unused").Scan(&unusedLeft))
	assert.Equal(t, 1, ancestorLeft, "cleanup must keep an expired consumed ancestor while descendants are live")
	assert.Zero(t, unusedLeft, "cleanup must still drop an expired refresh token that was never consumed")

	_, err = store.rotateRefreshToken(ctx, RefreshExchange{
		ClientID: client.ID, RefreshToken: ancestor.RefreshToken,
	}, replayAt, time.Hour, 30*24*time.Hour)
	assert.ErrorIs(t, err, ErrRefreshReuse)

	for _, token := range []string{replacement.RefreshToken, child.RefreshToken} {
		_, refreshErr := store.rotateRefreshToken(ctx, RefreshExchange{
			ClientID: client.ID, RefreshToken: token,
		}, replayAt, time.Hour, 30*24*time.Hour)
		assert.ErrorIs(t, refreshErr, ErrRefreshReuse)
	}
	_, err = store.validateAccessToken(ctx, replacement.AccessToken, testResource, replayAt)
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = store.validateAccessToken(ctx, child.AccessToken, testResource, replayAt)
	assert.ErrorIs(t, err, ErrInvalidToken)

	_, err = store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID: client.ID, Subject: "user", RedirectURI: testRedirect,
		CodeChallenge: "challenge", Resource: testResource, Scope: "mcp",
	}, replayAt, time.Minute)
	require.NoError(t, err)
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_tokens WHERE token_hash = ?`, hashSecret(ancestor.RefreshToken)).Scan(&ancestorLeft))
	assert.Zero(t, ancestorLeft, "consumed ancestor can be removed once its family has no live descendants")
}

func TestExpiredUnusedRefreshDoesNotRevokeActiveFamily(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC)
	client := Client{ID: "client-1", Name: "Claude", RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: now}
	require.NoError(t, store.createClient(ctx, client))

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	code, err := store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID: client.ID, Subject: "user", RedirectURI: testRedirect,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		Resource:      testResource, Scope: "mcp",
	}, now, 5*time.Minute)
	require.NoError(t, err)
	active, err := store.exchangeAuthorizationCode(ctx, CodeExchange{
		ClientID: client.ID, Code: code, RedirectURI: testRedirect,
		CodeVerifier: verifier, Resource: testResource,
	}, now, time.Hour, 30*24*time.Hour)
	require.NoError(t, err)

	replayAt := now.Add(time.Minute)
	unused := "expired-unused-refresh"
	_, err = store.db.ExecContext(ctx, `
INSERT INTO oauth_tokens (
    token_hash, token_type, family_id, client_id, subject, resource, scope,
    expires_at, created_at
) VALUES (?, 'refresh', 'expired-family', ?, 'user', ?, 'mcp', ?, ?)`,
		hashSecret(unused), client.ID, testResource, replayAt.Add(-time.Minute).Unix(), now.Unix())
	require.NoError(t, err)

	_, err = store.rotateRefreshToken(ctx, RefreshExchange{
		ClientID: client.ID, RefreshToken: unused,
	}, replayAt, time.Hour, 30*24*time.Hour)
	assert.ErrorIs(t, err, ErrInvalidGrant)

	rotated, err := store.rotateRefreshToken(ctx, RefreshExchange{
		ClientID: client.ID, RefreshToken: active.RefreshToken, Resource: testResource,
	}, replayAt, time.Hour, 30*24*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, rotated.RefreshToken)
	assert.NotEqual(t, active.RefreshToken, rotated.RefreshToken)
}

func TestConsumedRefreshRetentionIsBoundedAcrossRotations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	now := time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC)
	client := Client{ID: "client-1", Name: "Claude", RedirectURIs: []string{testRedirect}, ApplicationType: "web", TokenEndpointAuthMethod: "none", CreatedAt: now}
	require.NoError(t, store.createClient(ctx, client))

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abc"
	sum := sha256.Sum256([]byte(verifier))
	code, err := store.issueAuthorizationCode(ctx, AuthorizationGrant{
		ClientID: client.ID, Subject: "user", RedirectURI: testRedirect,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		Resource:      testResource, Scope: "mcp",
	}, now, 5*time.Minute)
	require.NoError(t, err)
	pair, err := store.exchangeAuthorizationCode(ctx, CodeExchange{
		ClientID: client.ID, Code: code, RedirectURI: testRedirect,
		CodeVerifier: verifier, Resource: testResource,
	}, now, time.Hour, refreshTokenTTL)
	require.NoError(t, err)

	// Rotate ten times per refresh-token lifetime for three lifetimes. A consumed
	// refresh token is kept at most one lifetime past its expiry, so only the 20
	// refresh tokens issued during the last two lifetimes remain.
	interval := refreshTokenTTL / 10
	for i := 1; i <= 30; i++ {
		pair, err = store.rotateRefreshToken(ctx, RefreshExchange{
			ClientID: client.ID, RefreshToken: pair.RefreshToken,
		}, now.Add(time.Duration(i)*interval), time.Hour, refreshTokenTTL)
		require.NoError(t, err)
	}

	var refreshRows int
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_tokens WHERE token_type = 'refresh'`).Scan(&refreshRows))
	assert.Equal(t, 20, refreshRows)
}

func TestLiveFamilyLookupUsesCompositeIndex(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	store, err := openStore("file:" + filepath.ToSlash(dbPath))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	// cleanupExpiredTx runs this lookup for every expired consumed refresh token,
	// so it must not scan the whole family through the family_id-only index.
	var id, parent, notUsed int
	var detail string
	require.NoError(t, store.db.QueryRowContext(ctx, `
EXPLAIN QUERY PLAN
SELECT 1 FROM oauth_tokens AS live
WHERE live.family_id = ? AND live.revoked_at IS NULL AND live.expires_at > ?`, "family", 0).Scan(&id, &parent, &notUsed, &detail))
	assert.Contains(t, detail, "idx_oauth_tokens_family_live")
}
