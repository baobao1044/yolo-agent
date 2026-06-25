# YOLO Mode — Background Permission Envelopes

YOLO Mode is YOLO Agent's answer to truly autonomous agents. Instead of waiting for a chat message, you give the agent an instruction, wrap it in a **permission envelope**, and let it work in the background while you do something else.

A permission envelope answers three questions:

1. **What can it do?** — scoped tool allow/deny lists.
2. **How much can it spend?** — token/call/run budgets.
3. **How do I know what happened?** — notifications on start/finish/error/budget.

## Why this is different

Most agents are chat-driven: you type, they reply, the loop ends. Background autonomous agents exist (AutoGPT, continuous workflows), but they are typically heavy Python stacks or cloud services. YOLO Mode gives you a **single Go binary** that can run scheduled or continuous tasks on your own machine with explicit, auditable limits.

## Core concepts

### Envelope

An envelope is a persisted task record stored in SQLite. It contains:

| Field | Meaning |
|-------|---------|
| `instruction` | The task the agent executes. |
| `schedule` | When to run: `@once`, `@interval 5m`, `@hourly`, `@daily`, or cron expression. |
| `scope.allowed_tools` | Tools the agent may use. Use `["*"]` with caution. |
| `scope.denied_tools` | Tools explicitly forbidden. |
| `budget.max_tokens` | Max LLM tokens per run (0 = unlimited). |
| `budget.max_calls` | Max tool calls per run (0 = unlimited). |
| `budget.max_duration` | Max runtime in seconds (0 = unlimited). |
| `notify.channels` | Where to notify: `tui`, `telegram`, `discord`, `slack`, `email`. |

### Lifecycle

```
pending → running → completed
   ↓        ↓         ↓
 paused   killed   failed
```

- `pending`: waiting for the next scheduled run.
- `running`: actively executing.
- `paused`: will not be scheduled again until resumed.
- `killed`: stopped by user or hit a hard limit.
- `completed/failed`: terminal states for one-shot or finished recurring tasks.

## Creating tasks

### From chat / TUI

Just ask the agent to schedule something:

```
每隔 10 分钟检查 https://example.com 的标题，如果变了就告诉我。允许 browser。预算 50 次调用。
```

The agent calls `schedule_task` with an envelope and starts working in the background.

### From HTTP API

```bash
curl -X POST http://localhost:8080/webhook \
  -H "Content-Type: application/json" \
  -d '{
    "action": "chat",
    "payload": {
      "message": "schedule a task named price-check to watch BTC price on binance.com every 5 minutes and notify me if it drops below 60000. allowed_tools: [\"browser\", \"respond\"]. max_calls: 20."
    }
  }'
```

### Task control endpoints

| Action | Payload | Effect |
|--------|---------|--------|
| `task_status` | `{}` or `{"id":"..."}` | List tasks or inspect one. |
| `task_pause` | `{"id":"..."}` | Pause scheduling and kill active run. |
| `task_resume` | `{"id":"..."}` | Resume scheduling. |
| `task_kill` | `{"id":"..."}` | Kill active run immediately. |

## Schedule expressions

| Expression | Meaning |
|------------|---------|
| `@once` or `now` | Run immediately, do not reschedule. |
| `@interval 5m` | Every 5 minutes. Supports `s`, `m`, `h`. |
| `@hourly` | At the top of every hour. |
| `@daily` | Midnight UTC every day. |
| `@weekly` | Midnight UTC every Sunday. |
| `@monthly` | Midnight UTC on the 1st of each month. |
| `*/30 * * * * *` | Standard cron with seconds field. |

## Safety

- **Denied tools always win.** If a tool is in both allowed and denied lists, it is denied.
- **Budgets are hard.** Exceeding `max_calls` or `max_duration` stops the task.
- **Scope is enforced by the registry.** Only tools in `allowed_tools` are exposed to the background agent.
- **Persistence.** Every envelope is stored in SQLite, so tasks survive restarts.

## Storage

Envelopes live in your configured SQLite database, table `background_envelopes`. You can query it directly:

```sql
SELECT id, name, schedule, status, run_count, total_calls, total_tokens
FROM background_envelopes
ORDER BY created_at DESC;
```

## Roadmap

- [ ] Token usage tracking per agent run.
- [ ] Resume a failed task from last checkpoint.
- [ ] Background notifications through messaging transports with default chat IDs.
- [ ] Cost estimation before accepting a schedule request.
- [ ] Web UI timeline of all background runs.
