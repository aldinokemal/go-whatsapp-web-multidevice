package mcp

import (
	"encoding/json"

	mcpg "github.com/mark3labs/mcp-go/mcp"
)

// structuredWithJSON keeps the human summary but appends the JSON payload to
// the text block: text-only MCP clients never read structuredContent, so a
// read tool that only summarizes returns nothing usable to them.
func structuredWithJSON(structured any, summary string) *mcpg.CallToolResult {
	payload, err := json.Marshal(structured)
	if err != nil {
		return mcpg.NewToolResultText(summary)
	}
	return mcpg.NewToolResultStructured(structured, summary+"\n"+string(payload))
}
