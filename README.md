# YOLO Agent

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License: MIT](https://img.shields.io/github/license/baobao1044/yolo-agent?color=blue)](LICENSE)
[![Build](https://img.shields.io/badge/build-passing-brightgreen)]()
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](CONTRIBUTING.md)

> **One binary. One agent. Full autonomy.** — A self-hosted AI agent in Go that drives your terminal, browser, and desktop, orchestrates multi-agent workflows, remembers everything, and now reasons over entire code repositories with budget-aware Code-RAG.

YOLO Agent is a lightweight, single-binary autonomous agent written in Go. It speaks the OpenAI-compatible LLM API, executes shell commands, automates Chrome, controls desktops over MCP, recalls long-term memory, schedules background tasks, and — new in v0.3 — assembles token-budgeted context from whole codebases using the **CORE** framework (MCKP context assembly + graduated SACRS representations).

---

## ✨ Features

### Core agent
- **Single binary** — `go build` and ship; no runtime, no containers
- **OpenAI-compatible LLM** — works with OpenAI, OpenRouter, and any compatible provider
- **Tool registry** — `respond`, `terminal`, `browser`, `computer`, `orchestrate`, `schedule_task`, `task_status`, `index_repo`, `repo_query`
- **Human-in-the-loop** — approval checkpoints for risky actions with configurable deny patterns
- **Skills** — reusable YAML skill definitions (prompt + tool allowlists)

### Computer use
- **Terminal** — shell command execution with timeouts and allow/deny lists
- **Browser** — Chrome automation via `chromedp` (navigate, click, type, screenshot, extract, eval JS)
- **Desktop** — control via MCP servers (`cua-driver`, `playwright-mcp`, …)

### Memory & retrieval
- **Long-term memory** — SQLite-backed vector memory with hybrid scoring
- **User profile** — auto-extracts and recalls user facts and preferences
- **Code-RAG (CORE)** — index whole repositories and retrieve budget-aware context (see [§ CORE Code-RAG](#-core-code-rag))

### Autonomy
- **YOLO Mode** — run background tasks with explicit tool, budget, and time limits
- **Cron scheduler** — schedule recurring tasks
- **Multi-agent workflows** — Ultracode-inspired orchestrator with up to 16 concurrent subagents and adversarial verification
- **MCP auto-discovery** — connect any Model Context Protocol server and its tools register automatically

### Interfaces
- **TUI** — terminal chat UI powered by Bubble Tea
- **HTTP Gateway** — REST + webhook endpoints (`/chat`, `/webhook`, `/status`)
- **Messaging Gateways** — Telegram, Discord, Slack RTM, SMTP email

---

## 🚀 Quick Start

### Build from source

```bash
# Set your LLM API key (OpenAI, OpenRouter, or any compatible provider)
export YOLO_API_KEY="your-api-key"

# Build the single binary
git clone https://github.com/baobao1044/yolo-agent.git
cd yolo-agent
go build -ldflags="-s -w" -o yolo-agent ./cmd/agent

# Create a default config
./yolo-agent --init
# Edit ~/.yolo-agent/config.yaml (or set YOLO_API_KEY env var)

# Launch the TUI
./yolo-agent

# Or run a single message and exit
./yolo-agent --one-shot "Hello, are you working?"
```

### Enable the HTTP gateway

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

> **Requirements:** Go 1.22+, Chrome/Chromium (for browser automation), optional MCP server binaries.

---

## 🧩 CORE Code-RAG

New in v0.3, YOLO Agent ships a **budget-aware repository context engine** implementing the CORE framework (*Budget-Aware Representation Selection for Repository-Scale Code Reasoning*). It indexes a codebase into a call/import graph, classifies symbols by architectural role, and at query time assembles a token-budgeted context by choosing graduated representation levels per symbol.

### Why it matters

Plain RAG dumps whole files into the context window — fine for a snippet, unusable for a 50k-LOC repo. CORE treats context assembly as a **Multi-Choice Knapsack Problem (MCKP)**: every symbol can be represented at four compression levels, each with a token cost and an information-retention value, and a greedy allocator picks the combination that maximizes retained information under your token budget.

### How it works

```
Query
  │
  ▼
Semantic rank (cosine over symbol embeddings)
  │
  ▼
BFS graph expansion  (keep nodes with S(v) ≥ θ)
  │
  ▼
Core / peripheral split  (seeds = core, pinned at raw)
  │
  ▼
Greedy MCKP allocation  (O(|V| log |V|), Algorithm 1)
  │
  ▼
SACRS materialization  (render each symbol at its chosen level)
  │
  ▼
Assembled context + per-symbol citations
```

### SACRS — four representation levels

| Level | k | What it keeps | Use when |
|---|---|---|---|
| **Raw** | 0 | Full source (signature + body + docs) | Core symbols you must fully see |
| **Sig + Docs** | 1 | Signature, return type, API docs | Need the contract, not the body |
| **Interface** | 2 | Signature only | Need the call shape |
| **Stub** | 3 | Symbol name only | Just need to know it exists |

### Compression priors R(k, Type)

Per the CORE doc's Table 1, each `NodeType × Level` carries an empirical retention value (fraction of reasoning-relevant information preserved). Defaults:

| Type | k=0 | k=1 | k=2 | k=3 |
|---|---|---|---|---|
| Controller | 1.00 | 0.95 | 0.80 | 0.40 |
| DTO | 1.00 | 0.92 | 0.75 | 0.35 |
| Service | 1.00 | 0.88 | 0.65 | 0.30 |
| Utility | 1.00 | 0.70 | 0.45 | 0.20 |
| Other | 1.00 | 0.75 | 0.50 | 0.25 |

The relevance score combines four factors:

```
S(v) = α·Semantic + β·EdgeWeight + γ·Centrality − δ·UtilityPenalty   (clamped to [0,1])
```

### Two tools

The agent calls these like any other tool:

- **`index_repo`** — walk a repo → parse symbols + edges → classify node types → compute graph degrees → embed → persist to SQLite. Skips unchanged repos by content hash.
- **`repo_query`** — given a natural-language question, run the full pipeline above and return assembled context with `tokens_used`, `nodes`, `core_count`, and `peripheral_count`.

### Try it

```yaml
# ~/.yolo-agent/config.yaml
corerag:
  enabled: true
  budget: 4096          # token budget for assembled context
  depth: 2              # BFS expansion depth
  embedding:
    provider: onnx      # onnx (local) | api | ollama | mock
    fallback: api        # used if the primary provider fails to init
    model: "bge-small-en-v1.5"
    dim: 384
    # model_path: "/path/to/model.onnx"   # for onnx; vocab.txt beside it
    # lib_path: "/path/to/libonnxruntime.so"  # or set YOLO_ONNX_LIB
  relevance:
    alpha: 0.4          # semantic similarity weight
    beta: 0.2           # edge weight
    gamma: 0.2          # centrality (out-degree) weight
    delta: 0.2          # utility penalty (in-degree) weight
    theta: 0.15         # S(v) >= theta required to expand a node
```

Then ask the agent: *"index the repo at /home/me/myproject and explain how the auth flow works."* The agent calls `index_repo`, then `repo_query`, and reasons over a budget-capped, citation-tagged slice of the codebase.

### Embedding backends

| Provider | When | Notes |
|---|---|---|
| **ONNX** (default) | Local, offline, most accurate | Requires `libonnxruntime` + a BGE/E5 model; build-tagged stub keeps the binary compiling with `CGO_ENABLED=0` |
| **API** | No local model, have an embeddings endpoint | Uses the OpenAI-compatible `/embeddings` route |
| **Ollama** | Local via `ollama serve` | Calls `localhost:11434/api/embeddings` |
| **Mock** | Tests / offline dev | Deterministic FNV-hash vectors, no dependencies |

The factory falls back from the primary provider to the configured fallback if init fails, so the engine keeps working when a model isn't installed.

See [docs/CORERAG.md](docs/CORERAG.md) for the full design, algorithms, and limitations.

---

## 🌙 YOLO Mode — Autonomous Background Tasks

YOLO Mode lets the agent work in the background with explicit guardrails called **permission envelopes**:

- **Scope** — allowed/denied tool lists
- **Budget** — max tokens, calls, runtime, and number of runs
- **Schedule** — `@once`, `@interval 5m`, `@hourly`, or cron expressions
- **Notifications** — status updates on start, finish, error, or budget hit

From chat:

```text
check https://example.com every 10 minutes and tell me if the title changes.
allowed tools: browser, respond. max calls: 50.
```

Via HTTP:

```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{"action":"chat","payload":{"message":"schedule a task named price-check to watch BTC price on binance.com every 5 minutes and notify me if it drops below 60000"}}'
```

See [docs/BACKGROUND.md](docs/BACKGROUND.md) for details.

---

## 🛠️ Tools

| Tool | Description |
|---|---|
| `respond` | Send a final response to the user and end the loop |
| `terminal` | Execute a shell command with a configurable timeout |
| `browser` | Automate Chrome: navigate, click, type, screenshot, extract, eval JS |
| `computer` | Control the desktop via MCP: click, type, screenshot, scroll, focus, a11y tree |
| `orchestrate` | Trigger a multi-agent workflow with phases, parallel subagents, and adversarial verification |
| `index_repo` | Index a repository for Code-RAG (parse → classify → embed → persist) |
| `repo_query` | Retrieve budget-aware context from an indexed repo for a natural-language query |
| `schedule_task` | Create a background task with a permission envelope *(YOLO Mode)* |
| `task_status` | Inspect running and historical background tasks and their budgets |

---

## 🏗️ Architecture

```
┌─────────┐     ┌──────────────────┐     ┌──────────────────┐
│  User   │────▶│  CLI / TUI /     │────▶│   Agent Loop      │
│         │     │  HTTP / Bots     │     │   LLM + Tools    │
└─────────┘     └──────────────────┘     └────────┬─────────┘
                                                  │ tool calls
                          ┌───────────────────────┼───────────────────────┐
                          ▼                       ▼                       ▼
                   ┌────────────┐          ┌────────────┐         ┌──────────────┐
                   │ Terminal / │          │  Browser   │         │  Code-RAG    │
                   │ Computer / │          │ (chromedp) │         │ (CORE engine)│
                   │ MCP        │          └────────────┘         └──────────────┘
                   └────────────┘                                          │
                          │                                                 ▼
                          ▼                                          SQLite index
                   ┌────────────────────┐                          (symbols/edges)
                   │  Workflow Engine    │
                   │  Orchestrator +     │
                   │  Subagents + Verify │
                   └────────────────────┘
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full architecture, data flow, and design principles.

---

## ⚙️ Configuration

See [`config.example.yaml`](config.example.yaml) for the full annotated template. Key sections:

```yaml
llm:
  base_url: "https://openrouter.ai/api/v1"
  api_key: ""           # or set YOLO_API_KEY
  model: "anthropic/claude-3.5-sonnet"
  max_tokens: 4096
  temperature: 0.7

agent:
  max_iterations: 50
  system_prompt: ""    # empty = built-in default

memory:
  driver: sqlite
  database: ""         # empty = ~/.yolo-agent/memory.db
  fts_enabled: true

corerag:
  enabled: false       # set true to enable Code-RAG (see § CORE Code-RAG)

workflow:
  max_concurrent: 16
  verify_retries: 2

approval:
  auto_approve_low_risk: true
  require_approval_for: [terminal, computer, mcp_]
  deny_patterns:
    - '(?i)rm\s+-rf\s*/'
    - '(?i)mkfs'
    - '(?i)dd\s+if=.*of=/dev/'
```

### Environment variables

| Variable | Description |
|---|---|
| `YOLO_API_KEY` | LLM API key (overrides `llm.api_key`) |
| `YOLO_CONFIG` | Path to config file |
| `YOLO_ONNX_LIB` | Path to `libonnxruntime` for ONNX embeddings |

---

## 🔒 Safety

- Risky terminal commands require approval by default
- Deny patterns block dangerous commands (`rm -rf /`, `mkfs`, `dd of=/dev/...`)
- Desktop control runs through MCP servers for sandbox separation
- YOLO Mode enforces explicit scope + budget + time limits per background task
- All tool calls are logged to the SQLite audit table

---

## 📚 Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [CORE Code-RAG](docs/CORERAG.md) — the budget-aware repository context engine
- [YOLO Mode — Background Tasks](docs/BACKGROUND.md)
- [Getting Started](docs/GETTING_STARTED.md)
- [MCP Auto-Discovery](docs/MCP.md)
- [Memory & RAG](docs/MEMORY.md)
- [Workflows](docs/WORKFLOW.md)
- [Human-in-the-Loop Approvals](docs/APPROVALS.md)
- [Gateways](docs/GATEWAY.md)

---

## 🧪 Development

```bash
go build ./...
go vet ./...
go test ./...
```

The pure-Go path compiles with `CGO_ENABLED=0` (ONNX/tree-sitter use build-tagged stubs). Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).

---

## 📄 License

MIT — see [LICENSE](LICENSE) for details.

## 🙏 Acknowledgments

- Inspired by [Hermes Agent](https://github.com/NousResearch/hermes-agent), [Claude Code](https://code.claude.com), and [Ultracode mode](https://code.claude.com/docs/en/workflows)
- CORE Code-RAG implements the framework from the *Budget-Aware Representation Selection for Repository-Scale Code Reasoning* technical document
- Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea), [chromedp](https://github.com/chromedp/chromedp), [go-openai](https://github.com/sashabaranov/go-openai), [modernc.org/sqlite](https://gitlab.com/cznic/sqlite), and [onnxruntime_go](https://github.com/yalue/onnxruntime_go)
