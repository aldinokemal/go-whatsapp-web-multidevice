package cmd

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	mcpoauth "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.PersistentFlags().BoolVar(
		&config.McpOAuthEnabled,
		"mcp-oauth-enabled",
		config.McpOAuthEnabled,
		"enable OAuth 2.1 for the MCP endpoint",
	)
	rootCmd.PersistentFlags().StringVar(
		&config.McpOAuthIssuerURL,
		"mcp-oauth-issuer-url",
		config.McpOAuthIssuerURL,
		"public HTTPS OAuth issuer URL, e.g. https://gowa.example.com",
	)
	rootCmd.PersistentFlags().StringVar(
		&config.McpOAuthResourceURL,
		"mcp-oauth-resource-url",
		config.McpOAuthResourceURL,
		"canonical public MCP resource URL; defaults to issuer origin + APP_BASE_PATH + /mcp",
	)
	rootCmd.PersistentFlags().StringVar(
		&config.McpOAuthDBURI,
		"mcp-oauth-db-uri",
		config.McpOAuthDBURI,
		"SQLite URI for OAuth clients, authorization codes, and tokens",
	)
}

// loadMcpOAuthEnvConfig runs from restServer after Cobra has parsed flags. It
// mirrors root.go's Viper binding while preserving the project's flag > env >
// .env priority for the OAuth settings added in this feature.
func loadMcpOAuthEnvConfig() {
	flags := rootCmd.PersistentFlags()
	if flag := flags.Lookup("mcp-oauth-enabled"); flag == nil || !flag.Changed {
		if viper.IsSet("mcp_oauth_enabled") {
			config.McpOAuthEnabled = viper.GetBool("mcp_oauth_enabled")
		}
	}
	if flag := flags.Lookup("mcp-oauth-issuer-url"); flag == nil || !flag.Changed {
		if value := strings.TrimSpace(viper.GetString("mcp_oauth_issuer_url")); value != "" {
			config.McpOAuthIssuerURL = value
		}
	}
	if flag := flags.Lookup("mcp-oauth-resource-url"); flag == nil || !flag.Changed {
		if value := strings.TrimSpace(viper.GetString("mcp_oauth_resource_url")); value != "" {
			config.McpOAuthResourceURL = value
		}
	}
	if flag := flags.Lookup("mcp-oauth-db-uri"); flag == nil || !flag.Changed {
		if value := strings.TrimSpace(viper.GetString("mcp_oauth_db_uri")); value != "" {
			config.McpOAuthDBURI = value
		}
	}
}

// registerMcpOAuth must run before the application's global Basic Auth
// middleware is installed so standards discovery, authorization, registration,
// and token endpoints remain reachable by OAuth clients.
func registerMcpOAuth(app *fiber.App, dm *whatsapp.DeviceManager) (*mcpoauth.Server, bool, error) {
	loadMcpOAuthEnvConfig()
	if !config.McpEnabled || !config.McpOAuthEnabled {
		return nil, false, nil
	}

	validateCredential, err := mcpOAuthCredentialValidator(config.AppBasicAuthCredential)
	if err != nil {
		return nil, false, err
	}

	resourceURL := strings.TrimSpace(config.McpOAuthResourceURL)
	if resourceURL == "" {
		resourceURL, err = mcpoauth.DefaultResourceURL(config.McpOAuthIssuerURL, config.AppBasePath)
		if err != nil {
			return nil, false, err
		}
	}

	oauthServer, err := mcpoauth.New(mcpoauth.Config{
		IssuerURL:   config.McpOAuthIssuerURL,
		ResourceURL: resourceURL,
		StorageURI:  config.McpOAuthDBURI,
	}, validateCredential)
	if err != nil {
		return nil, false, err
	}

	oauthServer.RegisterPublic(app)

	var mcpRouter fiber.Router = app
	if config.AppBasePath != "" {
		mcpRouter = app.Group(config.AppBasePath)
	}
	mcpRouter = mcpRouter.Group("", oauthServer.MCPAuthMiddleware(validateCredential))
	uimcp.Register(mcpRouter, dm, uimcp.Deps{
		App:     appUsecase,
		Send:    sendUsecase,
		Chat:    chatUsecase,
		User:    userUsecase,
		Message: messageUsecase,
		Group:   groupUsecase,
	})

	return oauthServer, true, nil
}

func mcpOAuthCredentialValidator(credentials []string) (mcpoauth.CredentialValidator, error) {
	if len(credentials) == 0 {
		return nil, errors.New("MCP OAuth requires APP_BASIC_AUTH credentials")
	}
	accounts := make(map[string]string, len(credentials))
	for _, credential := range credentials {
		parts := strings.Split(credential, ":")
		if len(parts) != 2 || parts[0] == "" {
			return nil, fmt.Errorf("basic auth is not valid, use <user>:<secret>")
		}
		accounts[parts[0]] = parts[1]
	}
	return func(username, password string) bool {
		expected, ok := accounts[username]
		if !ok {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(password), []byte(expected)) == 1
	}, nil
}
