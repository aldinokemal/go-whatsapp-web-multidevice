package cmd

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mcpRPC(t *testing.T, app *fiber.App, body string, auth bool) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth {
		req.SetBasicAuth("user", "secret")
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode

	var decoded map[string]any
	if len(raw) > 0 && resp.StatusCode == 200 {
		// streamable HTTP may answer as JSON or as a single SSE event
		payload := string(raw)
		if strings.HasPrefix(payload, "event:") || strings.HasPrefix(payload, "data:") {
			for _, line := range strings.Split(payload, "\n") {
				if strings.HasPrefix(line, "data:") {
					payload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
					break
				}
			}
		}
		require.NoError(t, json.Unmarshal([]byte(payload), &decoded), "body: %s", string(raw))
	}
	return rec, decoded
}

const initializeRPC = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`
const toolsListRPC = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`

func newMcpTestApp(withAuth bool) *fiber.App {
	app := fiber.New()
	if withAuth {
		app.Use(newBasicAuthMiddleware(map[string]string{"user": "secret"}))
	}
	// nil DeviceManager: tools/list never touches devices, and the context
	// func guards nil. Empty deps: tools/list never calls usecases.
	uimcp.Register(app, nil, uimcp.Deps{})
	return app
}

func TestMcpEndpointListsFiveTools(t *testing.T) {
	app := newMcpTestApp(false)

	rec, initRes := mcpRPC(t, app, initializeRPC, false)
	require.Equal(t, 200, rec.Code)
	require.NotNil(t, initRes["result"])

	rec, listRes := mcpRPC(t, app, toolsListRPC, false)
	require.Equal(t, 200, rec.Code)
	result, ok := listRes["result"].(map[string]any)
	require.True(t, ok, "tools/list result: %v", listRes)
	tools, ok := result["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 5)

	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"whatsapp_send", "whatsapp_message", "whatsapp_chat", "whatsapp_group", "whatsapp_app"} {
		assert.True(t, names[want], "missing tool %s", want)
	}
}

func TestMcpEndpointRequiresBasicAuth(t *testing.T) {
	app := newMcpTestApp(true)

	rec, _ := mcpRPC(t, app, toolsListRPC, false)
	assert.Equal(t, 401, rec.Code)

	rec, _ = mcpRPC(t, app, initializeRPC, true)
	assert.Equal(t, 200, rec.Code)
}
