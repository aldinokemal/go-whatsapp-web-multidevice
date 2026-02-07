#!/usr/bin/env node
/**
 * GoWA MCP Server
 * 
 * Wraps GoWA REST API and exposes it via MCP protocol.
 * Supports both stdio (local AI assistants) and HTTP (remote access) transports.
 * 
 * @author GoWA Contributors
 * @license MIT
 * @see https://github.com/aldinokemal/go-whatsapp-web-multidevice
 */

import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { SSEServerTransport } from "@modelcontextprotocol/sdk/server/sse.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { createServer, IncomingMessage, ServerResponse } from "http";
import { tools } from "./tools.js";
import { handleTool, Config } from "./handlers.js";

// Configuration from environment
const config: Config = {
  gowaUrl: process.env.GOWA_URL || "http://localhost:3000",
  deviceId: process.env.GOWA_DEVICE_ID || "",
};

const httpConfig = {
  port: parseInt(process.env.MCP_HTTP_PORT || "8090", 10),
  host: process.env.MCP_HTTP_HOST || "0.0.0.0",
};

// Parse CLI args
const cliArgs = process.argv.slice(2);
const useHttp = cliArgs.includes("--http") || cliArgs.includes("-h");
const showHelp = cliArgs.includes("--help");

// Main server
async function main() {
  const server = new Server(
    {
      name: "whatsapp-mcp",
      version: "1.0.0",
    },
    {
      capabilities: {
        tools: {},
      },
    }
  );

  // List tools handler
  server.setRequestHandler(ListToolsRequestSchema, async () => {
    return { tools };
  });

  // Call tool handler
  server.setRequestHandler(CallToolRequestSchema, async (request) => {
    const { name, arguments: args } = request.params;

    try {
      const result = await handleTool(config, name, (args as Record<string, unknown>) || {});
      return {
        content: [
          {
            type: "text",
            text: JSON.stringify(result, null, 2),
          },
        ],
      };
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      return {
        content: [
          {
            type: "text",
            text: `Error: ${errorMessage}`,
          },
        ],
        isError: true,
      };
    }
  });

  if (useHttp) {
    // HTTP/SSE transport for remote access
    await startHttpServer(server);
  } else {
    // stdio transport for local AI assistants
    const transport = new StdioServerTransport();
    await server.connect(transport);
    console.error("GoWA MCP Server running on stdio");
    console.error(`GoWA URL: ${config.gowaUrl}`);
    console.error(`Device ID: ${config.deviceId || "(auto)"}`);
  }
}

// HTTP Server for remote MCP access
async function startHttpServer(server: Server) {
  const transports = new Map<string, SSEServerTransport>();

  const httpServer = createServer(async (req: IncomingMessage, res: ServerResponse) => {
    // CORS headers
    res.setHeader("Access-Control-Allow-Origin", "*");
    res.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
    res.setHeader("Access-Control-Allow-Headers", "Content-Type");

    if (req.method === "OPTIONS") {
      res.writeHead(200);
      res.end();
      return;
    }

    const url = new URL(req.url || "/", `http://${req.headers.host}`);

    // Health check
    if (url.pathname === "/health") {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ status: "ok", transport: "http-sse" }));
      return;
    }

    // SSE endpoint for MCP
    if (url.pathname === "/sse" || url.pathname === "/mcp") {
      const transport = new SSEServerTransport("/message", res);
      const sessionId = crypto.randomUUID();
      transports.set(sessionId, transport);

      res.on("close", () => {
        transports.delete(sessionId);
      });

      await server.connect(transport);
      return;
    }

    // Message endpoint for MCP
    if (url.pathname === "/message" && req.method === "POST") {
      let body = "";
      req.on("data", (chunk) => (body += chunk));
      req.on("end", async () => {
        // Find active transport and handle message
        for (const transport of transports.values()) {
          try {
            await transport.handlePostMessage(req, res, body);
            return;
          } catch {
            // Try next transport
          }
        }
        res.writeHead(400, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "No active session" }));
      });
      return;
    }

    // 404 for other routes
    res.writeHead(404, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ 
      error: "Not found",
      endpoints: {
        "/health": "Health check",
        "/sse": "SSE endpoint for MCP connection",
        "/mcp": "Alias for /sse",
        "/message": "POST endpoint for MCP messages"
      }
    }));
  });

  httpServer.listen(httpConfig.port, httpConfig.host, () => {
    console.error(`GoWA MCP Server running on HTTP`);
    console.error(`  SSE endpoint: http://${httpConfig.host}:${httpConfig.port}/sse`);
    console.error(`  Message endpoint: http://${httpConfig.host}:${httpConfig.port}/message`);
    console.error(`  Health: http://${httpConfig.host}:${httpConfig.port}/health`);
    console.error(`GoWA URL: ${config.gowaUrl}`);
    console.error(`Device ID: ${config.deviceId || "(auto)"}`);
  });
}

// Help text
if (showHelp) {
  console.log(`
GoWA MCP Server - WhatsApp API for AI Assistants

Usage:
  gowa-mcp [options]

Options:
  --http, -h    Start HTTP/SSE server instead of stdio
  --help        Show this help message

Environment Variables:
  GOWA_URL          GoWA REST API URL (default: http://localhost:3000)
  GOWA_DEVICE_ID    Default device ID (optional, auto-detected if single device)
  MCP_HTTP_PORT     HTTP server port (default: 8090)
  MCP_HTTP_HOST     HTTP server host (default: 0.0.0.0)

Examples:
  # stdio mode (for Windsurf, Cursor, Claude Desktop)
  gowa-mcp

  # HTTP mode (for remote access)
  gowa-mcp --http

  # With custom GoWA URL
  GOWA_URL=http://192.168.1.100:3000 gowa-mcp
`);
  process.exit(0);
}

main().catch((error) => {
  console.error("Fatal error:", error);
  process.exit(1);
});
