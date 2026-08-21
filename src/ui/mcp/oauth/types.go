package oauth

import (
	"errors"
	"time"
)

var (
	ErrInvalidGrant      = errors.New("invalid grant")
	ErrInvalidTarget     = errors.New("invalid target")
	ErrInvalidToken      = errors.New("invalid token")
	ErrRefreshReuse      = errors.New("refresh token reuse detected")
	ErrRegistrationLimit = errors.New("dynamic client registration limit reached")
)

type CredentialValidator func(username, password string) bool

type Config struct {
	IssuerURL   string
	ResourceURL string
	StorageURI  string
}

type Client struct {
	ID                      string
	Name                    string
	RedirectURIs            []string
	ApplicationType         string
	TokenEndpointAuthMethod string
	CreatedAt               time.Time
}

type AuthorizationGrant struct {
	ClientID      string
	Subject       string
	RedirectURI   string
	CodeChallenge string
	Resource      string
	Scope         string
}

type CodeExchange struct {
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
	Resource     string
}

type RefreshExchange struct {
	ClientID     string
	RefreshToken string
	Resource     string
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
	Scope        string
}

type Principal struct {
	Subject  string
	ClientID string
	Scope    string
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}
