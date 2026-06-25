# YOLO Agent Architecture

YOLO Agent is designed as a lightweight, modular autonomous AI agent. This document explains the internal architecture, data flow, and design decisions.

## High-level Overview

```
┌─────────────┐     ┌─────────────────────────────────────────────┐
│   User      │────▶│  CLI / TUI / HTTP / Telegram / Discord / ... │
└─────────────┘     └─────────────────────────────────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │   AI Agent Loop  │
                    │  LLM → Tools     │
                    └────────┬─────────┘
                             │ tool calls
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
        ┌──────────┐   ┌──────────┐   ┌──────────┐
        │ Terminal │   │ Browser  │   │Computer/ │
        │          │   │(chromedp)│   │ MCP     │
        └──────────┘   └──────────┘   └──────────┘
                             │
                             ▼
                ┌────────────────────┐
                │ Workflow Engine    │
                │ Orchestrator       │
                │ Subagents + Verify │
                └────────────────────┘
```

## Core Components

### 1. Agent Loop (`internal/agent`)

```
User Input
   │
   ▼
 ┌──────────────────────────┐
 │  AIAgent                 │
 │  system prompt + skills  │
 │  conversation history    │
 │  memory recall           │
 └──────────┬───────────────┘
            │ messages + tools
            ▼
      ┌──────────┐
      │  LLM     │
      └────┬─────┘
           │ tool calls
           ▼
     Tool Registry
           │
     execute tool
           │
     result loop back
```

- The agent loop is event-driven and tool-oriented.
- It keeps a conversation history stored in SQLite.
- Before each LLM call, relevant memories and user profile facts are injected.

### 2. Tool Registry (`internal/tools`)

Every capability is a `Tool`:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() ToolSchema
    Execute(ctx context.Context, args json.RawMessage) (any, error)
}
```

Tools are registered at runtime:
- Built-in: `respond`, `terminal`, `browser`, `computer`, `orchestrate`
- Dynamic: discovered from MCP servers (`mcp_<server>_<tool>`)

### 3. Memory System (`internal/memory`)

| Layer | Purpose |
|---|---|
| `Store` | SQLite persistence for messages, conversations, audit logs |
| `VectorStore` | Long-term memory with cosine similarity recall |
| `ProfileStore` | User profile extraction and recall |

Memory scoring:

```
score = 0.6 * cosine_similarity + 0.25 * recency + 0.15 * importance
```

### 4. Workflow Engine (`internal/workflow`)

Ultracode-inspired orchestration:

1. LLM calls `orchestrate` tool with a JSON plan
2. Workflow engine parses phases and dependencies
3. Each task runs as an independent subagent in a goroutine pool
4. `verify` phases run adversarially against change outputs
5. Final answer is merged and returned to the main context

### 5. MCP Manager (`internal/mcp`)

Connects to multiple MCP servers over stdio. Each server is queried with `tools/list`, and tools are wrapped into the internal `Tool` interface with JSON schema conversion.

### 6. Approval Checkpoint (`internal/approvals`)

Before executing risky tools, the policy is evaluated. If risk is medium/high:

- An `ActionRequest` is created
- If interactive UI exists, prompt user for approval
- If not (one-shot mode), default to reject

Policies are configurable via YAML deny patterns.

### 7. Gateway Layer (`internal/gateway`)

Unified `Envelope` abstraction allows the same agent logic to respond to:

- TUI (`internal/ui`)
- HTTP API (`/chat`, `/webhook`, `/status`)
- Telegram bot
- Discord bot
- Slack RTM
- SMTP email

## Data Flow

### Typical chat request

```
User Input
  │
  ▼
Store message → memory extraction → profile update
  │
  ▼
Recall relevant memories + inject into context
  │
  ▼
LLM call with tools
  │
  ▼
Tool chosen → approval checkpoint (if risky) → execute
  │
  ▼
Store result in conversation + memory
  │
  ▼
Return response to user
```

### Workflow request

```
User asks a complex task
  │
  ▼
LLM calls orchestrate(plan)
  │
  ▼
Workflow Engine
  │
  ├── Phase 1: understand (parallel tasks)
  ├── Phase 2: change (depends on phase 1)
  └── Phase 3: verify (adversarial)
  │
  ▼
Final answer → user
```

## Design Principles

1. **Single binary** — easy to distribute
2. **Pluggable LLM** — OpenAI-compatible today, Ollama/Anthropic adapters later
3. **Tool-oriented** — everything is a tool; skills are prompt + tool allowlists
4. **Context preservation** — only summaries return to the main context
5. **Safety first** — approval checkpoints for destructive actions
6. **Gateway parity** — same agent logic across TUI, HTTP, and messaging bots
