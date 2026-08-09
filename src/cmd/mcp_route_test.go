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

// TestMcpEndpointGetDoesNotHang guards C1: a GET on /mcp must not reach
// mcp-go's standalone SSE notification stream, which blocks forever behind
// the fasthttp adaptor (ctx.Done() only fires on server shutdown, never on
// client disconnect). Only POST and DELETE are mounted, so GET must come
// back as a client error without app.Test's default timeout ever tripping.
func TestMcpEndpointGetDoesNotHang(t *testing.T) {
	app := newMcpTestApp(false)

	req := httptest.NewRequest("GET", "/mcp", nil)
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.True(t, resp.StatusCode == fiber.StatusNotFound || resp.StatusCode == fiber.StatusMethodNotAllowed,
		"GET /mcp returned %d, want 404 or 405", resp.StatusCode)
}

// TestMcpToolCallRejectsInvalidArgumentsViaSchema guards I1: with
// WithInputSchemaValidation enabled, a call missing a conditionally-required
// field (here: type=text without message) must be rejected by mcp-go's
// validator before the handler runs. The test app wires uimcp.Deps{} with a
// nil DeviceManager and nil usecases, so the handler itself would fail on
// device resolution first ("device identification required...") rather than
// panic — that generic tool error would make an isError:true assertion pass
// whether or not validation ran. So this test asserts on the actual
// validator error text ("input schema validation failed"), which can only
// appear if WithInputSchemaValidation rejected the call before the handler
// (and its device check) ever ran; the test must fail if validation is
// disabled again.
func TestMcpToolCallRejectsInvalidArgumentsViaSchema(t *testing.T) {
	app := newMcpTestApp(false)

	const callRPC = `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"whatsapp_send","arguments":{"type":"text","phone":"628"}}}`
	rec, res := mcpRPC(t, app, callRPC, false)
	require.Equal(t, 200, rec.Code)

	result, ok := res["result"].(map[string]any)
	require.True(t, ok, "tools/call response missing result: %v", res)
	require.Equal(t, true, result["isError"], "expected schema validation failure to surface as isError:true, got: %v", res)

	content, ok := result["content"].([]any)
	require.True(t, ok && len(content) > 0, "tools/call error result missing content: %v", res)
	text, _ := content[0].(map[string]any)["text"].(string)
	assert.Contains(t, text, "input schema validation failed",
		"expected mcp-go's validator to reject the missing 'message' field before the handler ran, got: %q", text)
	assert.Contains(t, text, "message", "expected the validator error to name the missing field, got: %q", text)
}
