# YOLO Agent

[![Go Version](https://img.shields.io/github/go-mod/go-version/baobg/yolo-agent)](https://go.dev/)
[![License](https://img.shields.io/github/license/baobg/yolo-agent)](LICENSE)

> **Autonomous AI Agent in Go** — terminal, browser, desktop control, multi-agent workflows, and long-term memory in a single binary.

YOLO Agent is a lightweight, self-hosted AI agent written in Go. It can execute terminal commands, automate browsers, control desktops via MCP, remember facts about users, and run Ultracode-inspired multi-agent workflows with adversarial verification.

---

## ✨ Features

- **Single binary** — easy to build and distribute
- **OpenAI-compatible LLM API** — works with OpenAI, OpenRouter, and other compatible providers
- **Tool registry** — `respond`, `terminal`, `browser`, `computer`, `orchestrate`
- **MCP auto-discovery** — connect to any Model Context Protocol server and register its tools automatically
- **Computer use**
  - Terminal/shell command execution
  - Browser automation via Chrome (`chromedp`)
  - Desktop control via MCP servers (e.g. `cua-driver`, `playwright-mcp`)
- **Long-term memory & RAG** — SQLite-backed vector memory with hybrid scoring
- **User profile** — auto-extracts and recalls user facts and preferences
- **Human-in-the-loop** — approval checkpoints for risky actions
- **Skills** — reusable YAML skill definitions
- **Cron scheduler** — run tasks on a schedule
- **HTTP Gateway** — REST and webhook endpoints
- **Messaging Gateways** — Telegram, Discord, Slack RTM, SMTP email
- **TUI** — terminal chat UI powered by Bubble Tea
- **Ultracode-inspired workflows**
  - Orchestrator decides when to fan out subagents
  - Up to 16 concurrent subagents
  - Adversarial verification phase with retries

---

## 🚀 Quick Start

### Installation

```bash
# Set API key
export YOLO_API_KEY="your-api-key"

# Create default config
yolo-agent --init
# Edit ~/.yolo-agent/config.yaml with your API key

# Run TUI
yolo-agent

# Run a single message
yolo-agent --one-shot "Hello"
```

### Build from source

```bash
git clone https://github.com/baobg/yolo-agent.git
cd yolo-agent
go build -ldflags="-s -w" -o yolo-agent ./cmd/agent
```

---

## 🏗️ Architecture

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full architecture overview.

```
┌─────────┐     ┌────────┐     ┌──────────────┐
│  User   │────▶│ CLI/   │────▶│  Agent Loop  │
│         │     │ Bot/   │     │  LLM + Tools │
└─────────┘     │ API/   │     └──────┬───────┘
                │ TUI    │            │
                └────────┘    ┌───────┴───────┐
                              ▼               ▼
                         ┌─────────┐   ┌──────────┐
                         │Terminal │   │ Browser  │
                         │Computer │   │ MCP      │
                         └─────────┘   └──────────┘
                              │
                              ▼
                    ┌────────────────────┐
                    │  Workflow Engine   │
                    │ Orchestrator + AI│
                    │ Subagents + Verify │
                    └────────────────────┘
```

---

## 📚 Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Getting Started](docs/GETTING_STARTED.md)
- [MCP Auto-Discovery](docs/MCP.md)
- [Memory & RAG](docs/MEMORY.md)
- [Workflows](docs/WORKFLOW.md)
- [Human-in-the-Loop Approvals](docs/APPROVALS.md)
- [Gateways](docs/GATEWAY.md)

---

## ⚙️ Configuration Example

```yaml
llm:
  base_url: "https://openrouter.ai/api/v1"
  api_key: ""           # or set YOLO_API_KEY env variable
  model: "anthropic/claude-3.5-sonnet"
  max_tokens: 4096
  temperature: 0.7

mcp_servers:
  - name: fetch
    command: python
    args:
      - -m
      - mcp_server_fetch

telegram:
  enabled: false
  token: ""

discord:
  enabled: false
  token: ""

slack:
  enabled: false
  token: ""

approval:
  auto_approve_low_risk: true
  require_approval_for:
    - terminal
    - computer
    - mcp_
  deny_patterns:
    - '(?i)rm\s+-rf\s*/'
    - '(?i)mkfs'
    - '(?i)dd\s+if=.*of=/dev/'
```

See [`config.example.yaml`](config.example.yaml) for the full template.

---

## 🛠️ Tools

### `respond`
Send a final response to the user and end the agent loop.

### `terminal`
Execute a shell command with configurable timeout.

### `browser`
Automate Chrome: navigate, click, type, screenshot, extract text, evaluate JS.

### `computer`
Control the desktop via MCP: click, type, screenshot, scroll, focus, accessibility tree.

### `orchestrate`
Trigger a multi-agent workflow with phases, parallel subagents, and adversarial verification.

---

## 🔒 Safety

- Risky terminal commands require approval by default.
- Deny patterns block dangerous commands.
- Desktop control runs through MCP servers for sandbox separation.
- All tool calls are logged to the SQLite audit table.

---

## 🤝 Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## 📄 License

This project is licensed under the MIT License — see [LICENSE](LICENSE) for details.

## 🙏 Acknowledgments

- Inspired by [Hermes Agent](https://github.com/NousResearch/hermes-agent), [Claude Code](https://code.claude.com), and [Ultracode mode](https://code.claude.com/docs/en/workflows)
- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [chromedp](https://github.com/chromedp/chromedp), [go-openai](https://github.com/sashabaranov/go-openai), and [modernc.org/sqlite](https://gitlab.com/cznic/sqlite)
