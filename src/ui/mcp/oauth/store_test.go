package oauth

import (
	"context"
	"crypto/sha256"
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
	defer rows.Close()
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
