# MCP Auto-Discovery

YOLO Agent supports the [Model Context Protocol (MCP)](https://modelcontextprotocol.io) for connecting external tools.

## What is MCP?

MCP is an open protocol that standardizes how AI applications connect to external data sources and tools. Each MCP server exposes:

- **Tools** — callable functions
- **Resources** — data endpoints
- **Prompts** — reusable templates

YOLO Agent currently focuses on **tools**.

## Configuration

Add MCP servers in your config:

```yaml
mcp_servers:
  - name: fetch
    command: python
    args:
      - -m
      - mcp_server_fetch
  - name: filesystem
    command: npx
    args:
      - -y
      - "@modelcontextprotocol/server-filesystem"
      - /tmp
```

## How it Works

1. On startup, YOLO Agent connects to each configured server over stdio.
2. Sends `initialize` handshake.
3. Calls `tools/list` to discover tools.
4. Each discovered tool becomes an internal tool named:

   ```
   mcp_<server>_<tool>
   ```

5. When the LLM calls a tool, YOLO Agent forwards the request via `tools/call`.

## Example

If you connect the `fetch` MCP server, the agent gains a tool like:

```json
{
  "name": "mcp_fetch_fetch",
  "description": "Fetches a URL and returns its content"
}
```

The LLM can then use it:

```json
{
  "tool": "mcp_fetch_fetch",
  "arguments": {"url": "https://example.com"}
}
```

## Security

- MCP tools are treated as medium-risk by default and may require approval.
- Deny dangerous patterns in the `approval.deny_patterns` config.
- Run MCP servers in isolated environments when possible.
