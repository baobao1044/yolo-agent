# Getting Started

## Installation

### Download prebuilt binary

See [GitHub Releases](https://github.com/baobg/yolo-agent/releases).

### Build from source

Requirements:
- Go 1.22+
- Chrome/Chromium (for browser automation)
- Optional: MCP server binaries

```bash
git clone https://github.com/baobg/yolo-agent.git
cd yolo-agent
go build -ldflags="-s -w" -o yolo-agent ./cmd/agent
```

## Quick Start

### 1. Create config

```bash
export YOLO_API_KEY="your-api-key"
./yolo-agent --init --config ~/.yolo-agent/config.yaml
```

Edit the config file to set your LLM API key if not using the env variable.

### 2. Run the TUI

```bash
./yolo-agent --config ~/.yolo-agent/config.yaml
```

### 3. Run a single message

```bash
./yolo-agent --one-shot "Hello, are you working?"
```

### 4. Enable HTTP gateway

```yaml
# ~/.yolo-agent/config.yaml
gateway:
  enabled: true
  port: 8080
```

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"payload": {"message": "Hello"}}'
```

## Configuration Examples

### Connect an MCP server

```yaml
mcp_servers:
  - name: fetch
    command: python
    args:
      - -m
      - mcp_server_fetch
```

### Enable Telegram bot

```yaml
telegram:
  enabled: true
  token: "YOUR_BOT_TOKEN"
```

### Approval policy

```yaml
approval:
  auto_approve_low_risk: true
  require_approval_for:
    - terminal
    - computer
    - mcp_
  deny_patterns:
    - '(?i)rm\s+-rf\s*/'
```

## Environment Variables

| Variable | Description |
|---|---|
| `YOLO_API_KEY` | LLM API key |
| `YOLO_CONFIG` | Path to config file |

## Next Steps

- Read [Architecture](ARCHITECTURE.md)
- Read [MCP](MCP.md)
- Read [Memory](MEMORY.md)
- Read [Workflows](WORKFLOW.md)
- Read [Approvals](APPROVALS.md)
- Read [Gateways](GATEWAY.md)
